#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIST="$ROOT/dist"
CACHE="$DIST/cache"
WORK=$(mktemp -d /tmp/frp-cluster-package.XXXXXX)
FRP_VERSION="${FRP_VERSION:-}"
ARCHES="${ARCHES:-amd64 arm64}"

mkdir -p "$DIST" "$CACHE"

if [[ -z "$FRP_VERSION" ]]; then
  FRP_VERSION=$(curl -fsSL https://api.github.com/repos/fatedier/frp/releases/latest | sed -n 's/.*"tag_name": *"\(v[^"]*\)".*/\1/p' | head -n 1)
fi
if [[ -z "$FRP_VERSION" ]]; then
  echo "failed to resolve latest frp version" >&2
  exit 1
fi
FRP_NUM="${FRP_VERSION#v}"

cd "$ROOT"
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache go test ./...
npm install --prefix web
npm run build --prefix web

for ARCH in $ARCHES; do
  PKG="frp-cluster-bundle-linux-$ARCH"
  PKGDIR="$WORK/$PKG"
  mkdir -p "$PKGDIR/bin" "$PKGDIR/scripts" "$PKGDIR/systemd" "$PKGDIR/examples" "$PKGDIR/docs" "$PKGDIR/web"

  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
    go build -buildvcs=false -trimpath -ldflags="-s -w" -o "$PKGDIR/bin/frp-cluster" ./cmd/frp-cluster

  FRP_ASSET="frp_${FRP_NUM}_linux_${ARCH}.tar.gz"
  FRP_CACHE="$CACHE/$FRP_ASSET"
  if [[ ! -s "$FRP_CACHE" ]]; then
    curl -fL --retry 5 --retry-delay 2 --retry-connrefused -o "$FRP_CACHE" "https://github.com/fatedier/frp/releases/download/${FRP_VERSION}/${FRP_ASSET}"
  fi
  tar -xzf "$FRP_CACHE" -C "$WORK"
  install -m 0755 "$WORK/frp_${FRP_NUM}_linux_${ARCH}/frps" "$PKGDIR/bin/frps"
  install -m 0755 "$WORK/frp_${FRP_NUM}_linux_${ARCH}/frpc" "$PKGDIR/bin/frpc"
  [[ -f "$WORK/frp_${FRP_NUM}_linux_${ARCH}/LICENSE" ]] && cp "$WORK/frp_${FRP_NUM}_linux_${ARCH}/LICENSE" "$PKGDIR/FRP-LICENSE"

  cp "$ROOT"/scripts/proxy-node-join.sh "$ROOT"/scripts/proxy-node-leave.sh "$ROOT"/scripts/client-install.sh "$PKGDIR/scripts/"
  chmod +x "$PKGDIR"/scripts/*.sh
  cp "$ROOT"/systemd/*.service "$PKGDIR/systemd/"
  cp "$ROOT"/examples/* "$PKGDIR/examples/"
  cp "$ROOT"/docs/*.md "$PKGDIR/docs/"
  cp -a "$ROOT"/web/dist/. "$PKGDIR/web/"
  cp "$ROOT/README.md" "$PKGDIR/README.md"

  cat > "$PKGDIR/README-bundle.md" <<EOF
# frp-cluster 一键安装包

目标平台：linux/$ARCH
frp 版本：$FRP_VERSION

## 代理服务器节点加入

cp examples/proxy-node.env.example proxy-node.env
vi proxy-node.env
sudo ./scripts/proxy-node-join.sh ./proxy-node.env

## 代理服务器节点退出

sudo ./scripts/proxy-node-leave.sh /etc/frp-cluster/node.env

## 客户端安装并开机自启

cp examples/client.env.example client.env
vi client.env
sudo ./scripts/client-install.sh ./client.env
EOF

  (cd "$WORK" && tar -czf "$DIST/$PKG.tar.gz" "$PKG")
  (cd "$DIST" && sha256sum "$PKG.tar.gz" > "$PKG.tar.gz.sha256")
done

ls -lh "$DIST"/frp-cluster-bundle-linux-*.tar.gz "$DIST"/frp-cluster-bundle-linux-*.sha256
