#!/bin/bash
# generate-config.sh — Generate a meridian.yaml config for a given node.
#
# Usage: ./generate-config.sh <node-id>
#   e.g.: ./generate-config.sh virginia
#
# Node names come from Terraform output — no hardcoded list.
# Redirect to a file: ./generate-config.sh virginia > virginia.yaml
set -euo pipefail
source "$(dirname "$0")/_common.sh"

if [ $# -lt 1 ]; then
  echo "Usage: $0 <node-id>" >&2
  echo "  Available nodes: $NODE_NAMES" >&2
  exit 1
fi

NODE_ID="$1"

if ! echo "$NODE_NAMES" | grep -qw "$NODE_ID"; then
  echo "Unknown node: $NODE_ID" >&2
  echo "Available: $NODE_NAMES" >&2
  exit 1
fi

API_KEY="${MERIDIAN_API_KEY:-CHANGE_ME}"

# Build cluster.nodes list dynamically from Terraform output
cluster_nodes=""
for name in $NODE_NAMES; do
  ip=$(node_ip "$name")
  lat=$(echo "$NODES_JSON" | jq -r --arg n "$name" '.[$n].lat')
  lng=$(echo "$NODES_JSON" | jq -r --arg n "$name" '.[$n].lng')
  cluster_nodes="${cluster_nodes}    - id: ${name}
      lat: ${lat}
      lng: ${lng}
      addr: \"http://${ip}:9090\"
"
done

cat <<EOF
node:
  id: "$NODE_ID"
  listen: ":8080"
  geoip_db: /opt/meridian/data/GeoLite2-City.mmdb

cluster:
  nodes:
${cluster_nodes}  health_check:
    interval: "10s"
    timeout: "3s"
    fail_threshold: 3

projects:
  - id: default
    api_key: "$API_KEY"
    origin: "https://example.com"
    hosts:
      - "proxy.meridian.sricharan.dev"
    cache:
      policy: sieve
      default_ttl: 3600
      max_size_mb: 256
      stale_while_revalidate: 30
      rules:
        - path: "/api/*"
          ttl: 0
EOF
