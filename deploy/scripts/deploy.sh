#!/bin/bash
# deploy.sh — Build locally, upload to all nodes in parallel, restart.
#
# Usage:
#   ./deploy.sh              # deploy to all nodes
#   ./deploy.sh virginia     # deploy to a single node
set -euo pipefail
source "$(dirname "$0")/_common.sh"

BINARY_NAME="meridian"
TARGET="${1:-all}"

# --- Cross-compile for Linux ---
echo "=== Building $BINARY_NAME for linux/amd64 ==="
cd "$PROJECT_ROOT"
GOOS=linux GOARCH=amd64 go build -o "/tmp/$BINARY_NAME" ./cmd/meridian
HASH=$(shasum -a 256 "/tmp/$BINARY_NAME" | cut -d' ' -f1)
echo "Binary hash: ${HASH:0:16}..."

# --- Generate configs (calls generate-config.sh, which inherits exported NODES_JSON) ---
for name in $NODE_NAMES; do
  "$SCRIPT_DIR/generate-config.sh" "$name" > "/tmp/meridian-${name}.yaml"
done

# --- Deploy ---

deploy_to() {
  local name="$1"
  local ip
  ip=$(node_ip "$name")

  echo "=== Deploying to $name ($ip) ==="

  scp $SSH_OPTS "/tmp/$BINARY_NAME" "/tmp/meridian-${name}.yaml" "ubuntu@$ip:/tmp/"

  ssh $SSH_OPTS "ubuntu@$ip" bash <<EOSSH
    sudo mv /tmp/$BINARY_NAME /opt/meridian/bin/$BINARY_NAME
    sudo mv /tmp/meridian-${name}.yaml /opt/meridian/config/meridian.yaml
    sudo chmod +x /opt/meridian/bin/$BINARY_NAME
    sudo chown -R meridian:meridian /opt/meridian/{bin,config}
    sudo systemctl restart meridian || echo "Service not configured yet — binary deployed but not started"
EOSSH
  echo "  done: $name"
}

if [ "$TARGET" != "all" ]; then
  if ! echo "$NODE_NAMES" | grep -qw "$TARGET"; then
    echo "Unknown node: $TARGET"
    echo "Valid: $NODE_NAMES all"
    exit 1
  fi
  deploy_to "$TARGET"
else
  for name in $NODE_NAMES; do
    deploy_to "$name" &
  done
  wait
fi

rm -f "/tmp/$BINARY_NAME"
for name in $NODE_NAMES; do rm -f "/tmp/meridian-${name}.yaml"; done

echo ""
echo "=== Deploy complete ($(date -u '+%Y-%m-%d %H:%M:%S UTC')) ==="
echo "Binary SHA-256: $HASH"
