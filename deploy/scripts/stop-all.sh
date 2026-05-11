#!/bin/bash
# stop-all.sh — Stop the Meridian service on all instances.
#
# Usage:
#   ./stop-all.sh               # stop service on all nodes
#   ./stop-all.sh --terminate   # stop service AND terraform destroy (with confirmation)
set -euo pipefail
source "$(dirname "$0")/_common.sh"

for name in $NODE_NAMES; do
  ip=$(node_ip "$name")
  echo "Stopping meridian on $name ($ip)..."
  ssh $SSH_OPTS -o ConnectTimeout=5 "ubuntu@$ip" \
    'sudo systemctl stop meridian 2>/dev/null; echo "stopped"' || echo "  (unreachable)"
done

echo ""
echo "All nodes stopped."

if [ "${1:-}" = "--terminate" ]; then
  echo ""
  echo "WARNING: This will run 'terraform destroy' and delete all instances,"
  echo "their Elastic IPs, Route 53 zone, and all associated resources."
  echo ""
  read -rp "Type 'destroy' to confirm: " confirm
  if [ "$confirm" = "destroy" ]; then
    cd "$TF_DIR"
    terraform destroy
  else
    echo "Aborted."
  fi
fi
