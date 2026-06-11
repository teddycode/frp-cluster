#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-/etc/frp-cluster/node.env}"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "用法: sudo ./scripts/proxy-node-leave.sh /etc/frp-cluster/node.env" >&2
  exit 2
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

: "${NODE_CONTROL_URL:?NODE_CONTROL_URL is required}"
: "${NODE_ID:?NODE_ID is required}"
: "${NODE_TOKEN:?NODE_TOKEN is required}"

/usr/local/bin/frp-cluster leave --control-url "$NODE_CONTROL_URL" --node-id "$NODE_ID" --token "$NODE_TOKEN"

systemctl disable --now frp-cluster-agent.service frps.service || true
echo "节点 $NODE_ID 已退出并停止 frps/agent。本地 Web 控制面仍保留，可用 systemctl disable --now frp-cluster-control 停止。"
