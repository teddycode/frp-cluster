#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE="${1:-./client.env}"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "用法: sudo ./scripts/client-install.sh ./client.env" >&2
  echo "可先复制 examples/client.env.example 并填写真实参数。" >&2
  exit 2
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

: "${CONTROL_URL:?CONTROL_URL is required}"
: "${CLIENT_ID:?CLIENT_ID is required}"

MODE="${MODE:-failover}"
LIMIT="${LIMIT:-1}"
INTERVAL="${INTERVAL:-30s}"
DRAIN_TIMEOUT="${DRAIN_TIMEOUT:-30s}"
WORK_DIR="${WORK_DIR:-/var/lib/frp-cluster/frpc.d}"
FRPC_BIN="${FRPC_BIN:-/usr/local/bin/frpc}"
PROXIES="${PROXIES:-}"

install -m 0755 ./bin/frp-cluster /usr/local/bin/frp-cluster
install -m 0755 ./bin/frpc "$FRPC_BIN"
mkdir -p /etc/frp-cluster "$WORK_DIR"

PROXY_ARGS=""
if [[ -n "$PROXIES" ]]; then
  IFS=';' read -r -a proxy_specs <<< "$PROXIES"
  for spec in "${proxy_specs[@]}"; do
    spec="${spec#"${spec%%[![:space:]]*}"}"
    spec="${spec%"${spec##*[![:space:]]}"}"
    if [[ -n "$spec" ]]; then
      PROXY_ARGS+=" --proxy $spec"
    fi
  done
fi

cat > /etc/frp-cluster/client.env <<ENVEOF
CONTROL_URL=$CONTROL_URL
CLIENT_ID=$CLIENT_ID
MODE=$MODE
LIMIT=$LIMIT
INTERVAL=$INTERVAL
DRAIN_TIMEOUT=$DRAIN_TIMEOUT
WORK_DIR=$WORK_DIR
FRPC_BIN=$FRPC_BIN
ENVEOF
proxy_args_escaped=$(printf "%s" "$PROXY_ARGS" | sed "s/'/'\\\\''/g")
printf "PROXY_ARGS='%s'\n" "$proxy_args_escaped" >> /etc/frp-cluster/client.env
chmod 0600 /etc/frp-cluster/client.env

cat > /usr/local/bin/frp-cluster-client-run <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC1091
source /etc/frp-cluster/client.env
args=(
  client
  --control-url "$CONTROL_URL"
  --client-id "$CLIENT_ID"
  --mode "$MODE"
  --limit "$LIMIT"
  --interval "$INTERVAL"
  --drain-timeout "$DRAIN_TIMEOUT"
  --work-dir "$WORK_DIR"
  --frpc-bin "$FRPC_BIN"
)
if [[ -n "${PROXY_ARGS:-}" ]]; then
  # shellcheck disable=SC2206
  extra=( $PROXY_ARGS )
  args+=("${extra[@]}")
fi
exec /usr/local/bin/frp-cluster "${args[@]}"
EOF
chmod 0755 /usr/local/bin/frp-cluster-client-run

install -m 0644 ./systemd/frp-cluster-client.service /etc/systemd/system/frp-cluster-client.service
systemctl daemon-reload
systemctl enable --now frp-cluster-client.service

echo "客户端 $CLIENT_ID 已安装并开机自启。"
echo "状态: systemctl status frp-cluster-client --no-pager"
