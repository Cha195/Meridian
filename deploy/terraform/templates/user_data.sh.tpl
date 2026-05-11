#!/bin/bash
# User data script — runs once on first boot to provision the instance.
# Terraform injects variables via templatefile().
set -euo pipefail
exec > /var/log/meridian-provision.log 2>&1

echo "=== Meridian provisioning: ${node_name} (${region}) ==="
echo "Started at $(date -u)"

# --- System packages ---
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y jq curl unzip

%{ if is_primary ~}
# --- Postgres (primary node only) ---
apt-get install -y postgresql postgresql-contrib

# Allow remote connections from peer nodes.
PG_HBA=$(find /etc/postgresql -name pg_hba.conf | head -1)
PG_CONF=$(find /etc/postgresql -name postgresql.conf | head -1)

# Listen on all interfaces (default is localhost only).
sed -i "s/#listen_addresses = 'localhost'/listen_addresses = '*'/" "$PG_CONF"

# Allow each peer node to connect with password auth.
%{ for ip in peer_ips ~}
echo "host    meridian    meridian    ${ip}/32    scram-sha-256" >> "$PG_HBA"
%{ endfor ~}

systemctl restart postgresql

# Create database and user (idempotent).
sudo -u postgres psql <<'EOSQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'meridian') THEN
    CREATE ROLE meridian WITH LOGIN PASSWORD '${db_password}';
  END IF;
END
$$;

SELECT 'CREATE DATABASE meridian OWNER meridian'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'meridian')\gexec
EOSQL
%{ endif ~}

# --- Meridian user and directories ---
useradd --system --shell /usr/sbin/nologin --home-dir /opt/meridian meridian || true
mkdir -p /opt/meridian/{bin,config,data}
chown -R meridian:meridian /opt/meridian

%{ if maxmind_license_key != "" ~}
# --- GeoLite2 City database ---
cd /tmp
curl -sSL "https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-City&license_key=${maxmind_license_key}&suffix=tar.gz" -o geolite2.tar.gz
tar xzf geolite2.tar.gz
find . -name 'GeoLite2-City.mmdb' -exec cp {} /opt/meridian/data/GeoLite2-City.mmdb \;
chown meridian:meridian /opt/meridian/data/GeoLite2-City.mmdb
rm -rf geolite2.tar.gz GeoLite2-City_*
%{ endif ~}

# --- Systemd service ---
cat > /etc/systemd/system/meridian.service <<'EOF'
[Unit]
Description=Meridian CDN Edge Node
After=network.target

[Service]
Type=simple
User=meridian
ExecStart=/opt/meridian/bin/meridian serve --config /opt/meridian/config/meridian.yaml
Restart=always
RestartSec=5
Environment=MERIDIAN_DEPLOYMENT_ID=%H

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable meridian

# --- Done ---
echo "$(date -u)" > /opt/meridian/.provisioned
echo "=== Provisioning complete: ${node_name} ==="
