# _common.sh — Shared helpers for deploy scripts.
# Source this file, don't execute it directly.
#
# Reads the `nodes` output from Terraform and provides:
#   NODES_JSON    — raw JSON of the nodes output (exported for child scripts)
#   NODE_NAMES    — space-separated list of node names (exported for child scripts)
#   node_ip()     — get IP for a node:  ip=$(node_ip virginia)
#   node_is_primary() — check if node is primary: if node_is_primary virginia; then ...
#   primary_ip()  — get the primary node's IP
#   all_ips()     — space-separated list of all IPs
#   MERIDIAN_API_KEY — project API key (exported)
#   TF_DIR, PROJECT_ROOT, SCRIPT_DIR — path helpers
#   SSH_OPTS      — common SSH options

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TF_DIR="$SCRIPT_DIR/../terraform"
PROJECT_ROOT="$SCRIPT_DIR/../.."
SSH_OPTS="-o StrictHostKeyChecking=accept-new"

_load_tf_outputs() {
  # Skip if already loaded (child scripts inherit exported vars)
  if [ -n "${NODES_JSON:-}" ]; then
    return
  fi

  if [ ! -f "$TF_DIR/terraform.tfstate" ]; then
    echo "ERROR: No terraform.tfstate found. Run 'terraform apply' first." >&2
    exit 1
  fi

  NODES_JSON=$(cd "$TF_DIR" && terraform output -json nodes)
  NODE_NAMES=$(echo "$NODES_JSON" | jq -r 'keys[]')
  MERIDIAN_API_KEY=$(cd "$TF_DIR" && terraform output -raw api_key 2>/dev/null) || MERIDIAN_API_KEY=""

  export NODES_JSON NODE_NAMES MERIDIAN_API_KEY
}

node_ip() {
  echo "$NODES_JSON" | jq -r --arg n "$1" '.[$n].ip'
}

node_is_primary() {
  [ "$(echo "$NODES_JSON" | jq -r --arg n "$1" '.[$n].is_primary')" = "true" ]
}

primary_ip() {
  echo "$NODES_JSON" | jq -r 'to_entries[] | select(.value.is_primary) | .value.ip'
}

all_ips() {
  echo "$NODES_JSON" | jq -r '.[].ip'
}

_load_tf_outputs
