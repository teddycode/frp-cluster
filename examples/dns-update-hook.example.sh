#!/usr/bin/env bash
set -euo pipefail

: "${FRP_CLUSTER_DNS_HOST:?FRP_CLUSTER_DNS_HOST is required}"
: "${FRP_CLUSTER_DNS_TARGET_IP:?FRP_CLUSTER_DNS_TARGET_IP is required}"

# Replace this file with a provider-specific implementation.
# It should update the A record for FRP_CLUSTER_DNS_HOST to FRP_CLUSTER_DNS_TARGET_IP.
echo "update ${FRP_CLUSTER_DNS_HOST} -> ${FRP_CLUSTER_DNS_TARGET_IP} node=${FRP_CLUSTER_NODE_ID:-} client=${FRP_CLUSTER_CLIENT_ID:-}"
