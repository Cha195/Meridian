#!/bin/bash
# setup-postgres.sh — Initialize the Meridian Postgres database.
# Runs the schema migration on the primary node.
#
# Usage: MERIDIAN_DB_PASSWORD=xxx ./setup-postgres.sh
#
# The password is read from MERIDIAN_DB_PASSWORD env var.
# If unset, it's fetched from terraform output.
# The primary node is determined from Terraform output.
#
# Idempotent: safe to run multiple times.
set -euo pipefail
source "$(dirname "$0")/_common.sh"

SCHEMA_FILE="$PROJECT_ROOT/schema/migrations/001_initial.sql"

if [ ! -f "$SCHEMA_FILE" ]; then
  echo "ERROR: Schema file not found at $SCHEMA_FILE"
  exit 1
fi

if [ -z "${MERIDIAN_DB_PASSWORD:-}" ]; then
  MERIDIAN_DB_PASSWORD=$(cd "$TF_DIR" && terraform output -raw db_password 2>/dev/null) || {
    echo "ERROR: Set MERIDIAN_DB_PASSWORD or ensure terraform output db_password is available."
    exit 1
  }
fi

IP=$(primary_ip)

echo "=== Setting up Postgres on primary node ($IP) ==="

scp $SSH_OPTS "$SCHEMA_FILE" "ubuntu@$IP:/tmp/001_initial.sql"

# Create user and database via separate psql -c calls to avoid heredoc
# quoting issues with $$ in PL/pgSQL blocks.
ssh $SSH_OPTS "ubuntu@$IP" bash <<EOSSH
set -euo pipefail

# Create meridian role if it doesn't exist
sudo -u postgres psql -c "DO \\\$\\\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'meridian') THEN
    CREATE ROLE meridian WITH LOGIN PASSWORD '${MERIDIAN_DB_PASSWORD}';
  END IF;
END
\\\$\\\$;"

# Create meridian database if it doesn't exist
sudo -u postgres psql -c "SELECT 'CREATE DATABASE meridian OWNER meridian' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'meridian')" | grep -q 'CREATE DATABASE' && \
  sudo -u postgres psql -c "CREATE DATABASE meridian OWNER meridian" || true

# Run the schema migration
PGPASSWORD="${MERIDIAN_DB_PASSWORD}" psql -h 127.0.0.1 -U meridian -d meridian -f /tmp/001_initial.sql

echo ""
echo "=== Tables in meridian database ==="
PGPASSWORD="${MERIDIAN_DB_PASSWORD}" psql -h 127.0.0.1 -U meridian -d meridian -c '\dt'

rm /tmp/001_initial.sql
EOSSH

echo ""
echo "=== Postgres setup complete ==="
