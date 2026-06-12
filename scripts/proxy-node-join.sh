#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE="${1:-./proxy-node.env}"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "用法: sudo ./scripts/proxy-node-join.sh ./proxy-node.env" >&2
  echo "可先复制 examples/proxy-node.env.example 并填写真实参数。" >&2
  exit 2
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

: "${BOOTSTRAP_CONTROL_URL:?BOOTSTRAP_CONTROL_URL is required}"
: "${NODE_CONTROL_URL:?NODE_CONTROL_URL is required}"
: "${NODE_ID:?NODE_ID is required}"
: "${PUBLIC_ADDR:?PUBLIC_ADDR is required}"

BIND_PORT="${BIND_PORT:-7000}"
ALLOW_PORTS="${ALLOW_PORTS:-11000-12000}"
VHOST_HTTP_PORT="${VHOST_HTTP_PORT:-0}"
VHOST_HTTPS_PORT="${VHOST_HTTPS_PORT:-0}"
CONTROL_LISTEN="${CONTROL_LISTEN:-:8080}"
CONTROL_DATA="${CONTROL_DATA:-/var/lib/frp-cluster/cluster.json}"
PUBLIC_ENTRY_HOST="${PUBLIC_ENTRY_HOST:-}"
DNS_UPDATE_HOOK="${DNS_UPDATE_HOOK:-}"
WEB_DIR="${WEB_DIR:-/usr/local/share/frp-cluster/web}"
ADMIN_PASSWORD_FILE="${ADMIN_PASSWORD_FILE:-/etc/frp-cluster/admin-password}"
AUTH_CONFIG_FILE="${AUTH_CONFIG_FILE:-/etc/frp-cluster/auth.env}"
ALIDNS_CONFIG_FILE="${ALIDNS_CONFIG_FILE:-/etc/frp-cluster/alidns.env}"
REGION="${REGION:-}"
TAGS="${TAGS:-}"
AGENT_INTERVAL="${AGENT_INTERVAL:-30s}"
PROBE_SIZE="${PROBE_SIZE:-262144}"
FRPS_DASHBOARD_URL="${FRPS_DASHBOARD_URL:-http://127.0.0.1:7500}"
JOIN_TOKEN="${JOIN_TOKEN:-auto}"

install -m 0755 ./bin/frp-cluster /usr/local/bin/frp-cluster
install -m 0755 ./bin/frps /usr/local/bin/frps
mkdir -p /etc/frp /etc/frp-cluster "$(dirname "$CONTROL_DATA")"
if [[ -d ./web ]]; then
  rm -rf "$WEB_DIR"
  mkdir -p "$WEB_DIR"
  cp -a ./web/. "$WEB_DIR/"
fi
if [[ ! -f "$ADMIN_PASSWORD_FILE" && "${ADMIN_PASSWORD:-}" != "" ]]; then
  install -m 0600 /dev/null "$ADMIN_PASSWORD_FILE"
  printf '%s\n' "$ADMIN_PASSWORD" > "$ADMIN_PASSWORD_FILE"
fi

cat > /etc/frp-cluster/node.env <<ENVEOF
BOOTSTRAP_CONTROL_URL=$BOOTSTRAP_CONTROL_URL
NODE_CONTROL_URL=$NODE_CONTROL_URL
NODE_ID=$NODE_ID
PUBLIC_ADDR=$PUBLIC_ADDR
BIND_PORT=$BIND_PORT
ALLOW_PORTS=$ALLOW_PORTS
VHOST_HTTP_PORT=$VHOST_HTTP_PORT
VHOST_HTTPS_PORT=$VHOST_HTTPS_PORT
CONTROL_LISTEN=$CONTROL_LISTEN
CONTROL_DATA=$CONTROL_DATA
PUBLIC_ENTRY_HOST=$PUBLIC_ENTRY_HOST
DNS_UPDATE_HOOK=$DNS_UPDATE_HOOK
WEB_DIR=$WEB_DIR
ADMIN_PASSWORD_FILE=$ADMIN_PASSWORD_FILE
AUTH_CONFIG_FILE=$AUTH_CONFIG_FILE
ALIDNS_CONFIG_FILE=$ALIDNS_CONFIG_FILE
REGION=$REGION
TAGS=$TAGS
AGENT_INTERVAL=$AGENT_INTERVAL
PROBE_SIZE=$PROBE_SIZE
FRPS_DASHBOARD_URL=$FRPS_DASHBOARD_URL
ENVEOF
chmod 0600 /etc/frp-cluster/node.env

install -m 0644 ./systemd/frp-cluster-control.service /etc/systemd/system/frp-cluster-control.service
install -m 0644 ./systemd/frps.service /etc/systemd/system/frps.service
install -m 0644 ./systemd/frp-cluster-agent.service /etc/systemd/system/frp-cluster-agent.service
systemctl daemon-reload
systemctl enable --now frp-cluster-control.service

for _ in $(seq 1 30); do
  if /usr/local/bin/frp-cluster health --control-url "$NODE_CONTROL_URL" --timeout 2s >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! /usr/local/bin/frp-cluster health --control-url "$NODE_CONTROL_URL" --timeout 2s >/dev/null 2>&1; then
  echo "本节点 Web 控制面未就绪: $NODE_CONTROL_URL" >&2
  exit 1
fi

if [[ "$JOIN_TOKEN" == "auto" ]]; then
  JOIN_TOKEN=$(/usr/local/bin/frp-cluster token --control-url "$NODE_CONTROL_URL" --ttl 10m --uses 1 --admin-password-file "$ADMIN_PASSWORD_FILE")
fi

JOIN_OUT=$(mktemp)
trap 'rm -f "$JOIN_OUT"' EXIT

JOIN_ARGS=(
  join
  --control-url "$BOOTSTRAP_CONTROL_URL"
  --token "$JOIN_TOKEN"
  --node-id "$NODE_ID"
  --public-addr "$PUBLIC_ADDR"
  --node-control-url "$NODE_CONTROL_URL"
  --bind-port "$BIND_PORT"
)
if [[ "$VHOST_HTTP_PORT" != "" && "$VHOST_HTTP_PORT" != "0" ]]; then
  JOIN_ARGS+=(--vhost-http-port "$VHOST_HTTP_PORT")
fi
if [[ "$VHOST_HTTPS_PORT" != "" && "$VHOST_HTTPS_PORT" != "0" ]]; then
  JOIN_ARGS+=(--vhost-https-port "$VHOST_HTTPS_PORT")
fi
if [[ -n "$REGION" ]]; then
  JOIN_ARGS+=(--region "$REGION")
fi
if [[ -n "$TAGS" ]]; then
  JOIN_ARGS+=(--tags "$TAGS")
fi

/usr/local/bin/frp-cluster "${JOIN_ARGS[@]}" | tee "$JOIN_OUT"
NODE_TOKEN=$(sed -n 's/^node_token=//p' "$JOIN_OUT" | tail -n 1)
if [[ -z "$NODE_TOKEN" ]]; then
  echo "join 输出里没有 node_token，无法启动 agent。" >&2
  exit 1
fi

cat >> /etc/frp-cluster/node.env <<ENVEOF
NODE_TOKEN=$NODE_TOKEN
ENVEOF
chmod 0600 /etc/frp-cluster/node.env

for _ in $(seq 1 30); do
  if /usr/local/bin/frp-cluster config frps --control-url "$NODE_CONTROL_URL" --node-id "$NODE_ID" > /etc/frp/frps.toml 2>/tmp/frp-cluster-frps-config.err; then
    chmod 0600 /etc/frp/frps.toml
    break
  fi
  sleep 1
done
if [[ ! -s /etc/frp/frps.toml ]]; then
  cat /tmp/frp-cluster-frps-config.err >&2 || true
  echo "无法从本节点 Web 控制面生成 frps 配置。" >&2
  exit 1
fi

if [[ -n "$ALLOW_PORTS" ]]; then
  allow_items=""
  IFS=',' read -r -a allow_ranges <<< "$ALLOW_PORTS"
  for range in "${allow_ranges[@]}"; do
    range="${range//[[:space:]]/}"
    if [[ -z "$range" ]]; then
      continue
    fi
    if [[ "$range" =~ ^([0-9]+)-([0-9]+)$ ]]; then
      item="{ start = ${BASH_REMATCH[1]}, end = ${BASH_REMATCH[2]} }"
    elif [[ "$range" =~ ^[0-9]+$ ]]; then
      item="{ single = $range }"
    else
      echo "ALLOW_PORTS 格式错误: $range，应使用 11000-12000 或 11022。" >&2
      exit 1
    fi
    if [[ -n "$allow_items" ]]; then
      allow_items+=", "
    fi
    allow_items+="$item"
  done
  if [[ -n "$allow_items" ]]; then
    frps_config_tmp=$(mktemp)
    {
      printf 'allowPorts = [%s]\n' "$allow_items"
      cat /etc/frp/frps.toml
    } > "$frps_config_tmp"
    install -m 0600 "$frps_config_tmp" /etc/frp/frps.toml
    rm -f "$frps_config_tmp"
  fi
fi

systemctl enable --now frps.service frp-cluster-agent.service
systemctl restart frp-cluster-control.service frps.service frp-cluster-agent.service

echo "节点 $NODE_ID 已加入。"
echo "本节点管理端: $NODE_CONTROL_URL"
echo "状态: systemctl status frp-cluster-control frps frp-cluster-agent --no-pager"
