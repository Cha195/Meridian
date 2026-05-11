#!/bin/bash
# status.sh — Check service status and health on all instances.
set -euo pipefail
source "$(dirname "$0")/_common.sh"

for name in $NODE_NAMES; do
  ip=$(node_ip "$name")
  primary_label=""
  if node_is_primary "$name"; then
    primary_label=" (primary)"
  fi
  echo "=== $name ($ip)${primary_label} ==="

  ssh $SSH_OPTS -o ConnectTimeout=5 "ubuntu@$ip" bash <<'EOSSH' 2>/dev/null || { echo "  (unreachable)"; echo ""; continue; }
    echo "Service: $(systemctl is-active meridian 2>/dev/null || echo 'not installed')"
    echo "Uptime:  $(systemctl show meridian --property=ActiveEnterTimestamp 2>/dev/null | cut -d= -f2 || echo 'n/a')"
    HEALTH=$(curl -s --connect-timeout 2 http://localhost:9090/healthz 2>/dev/null || echo '{"status":"unreachable"}')
    echo "Health:  $HEALTH"
    echo "Provisioned: $(cat /opt/meridian/.provisioned 2>/dev/null || echo 'not yet')"
EOSSH

  echo ""
done
