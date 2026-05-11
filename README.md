# Meridian

A 3-node edge CDN with pluggable cache eviction policies and a diagnostic dashboard.

## Deployment

### Prerequisites

- AWS account with credentials configured (`aws configure`)
- [Terraform](https://developer.hashicorp.com/terraform/install) >= 1.5
- SSH key pair at `~/.ssh/id_rsa` (or set `ssh_public_key_path` in tfvars)
- Go 1.22+ (for building the binary)

### Quick start

```bash
# 1. Configure
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars: set db_password, ssh_allowed_cidr, etc.

# 2. Deploy infrastructure
terraform init
terraform plan    # review what will be created
terraform apply   # creates 3 EC2 instances, Route 53 zone, Elastic IPs

# 3. Post-apply setup
# Add the NS records shown in output to your DNS provider (e.g. Vercel)
cd ../..
MERIDIAN_DB_PASSWORD=your_password deploy/scripts/setup-postgres.sh

# 4. Deploy the binary + configs to all 3 nodes
deploy/scripts/deploy.sh

# 5. Check status
deploy/scripts/status.sh
```

### Operations

```bash
deploy/scripts/deploy.sh              # deploy to all 3 nodes
deploy/scripts/deploy.sh virginia     # deploy to one node
deploy/scripts/status.sh              # check health on all nodes
deploy/scripts/stop-all.sh            # stop service on all nodes
deploy/scripts/stop-all.sh --terminate  # terraform destroy (with confirmation)
```

### Security notes

- `terraform.tfstate` contains the DB password. Never commit it.
- Port 9090 (admin API) is restricted to cluster node IPs only.
- Port 5432 (Postgres) is restricted to cluster node IPs only.
- Set `ssh_allowed_cidr` to your IP in production (`curl -s ifconfig.me`).