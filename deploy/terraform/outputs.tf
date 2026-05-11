# outputs.tf — Displayed after `terraform apply`.
#
# The `nodes` output is the single source of truth for deploy scripts.
# Scripts iterate over it instead of hardcoding node names.

output "nodes" {
  description = "All nodes with their IPs, regions, and roles. Scripts read this."
  value = {
    virginia = {
      ip         = aws_eip.virginia.public_ip
      region     = "us-east-1"
      is_primary = true
      lat        = 39.0481
      lng        = -77.4728
    }
    ireland = {
      ip         = aws_eip.ireland.public_ip
      region     = "eu-west-1"
      is_primary = false
      lat        = 53.3331
      lng        = -6.2489
    }
    tokyo = {
      ip         = aws_eip.tokyo.public_ip
      region     = "ap-northeast-1"
      is_primary = false
      lat        = 35.6762
      lng        = 139.6503
    }
  }
}

output "route53_nameservers" {
  description = "NS records to add to your DNS provider (e.g. Vercel)"
  value       = aws_route53_zone.main.name_servers
}

output "dns_delegation_instructions" {
  description = "How to wire up DNS"
  value       = <<-EOT

    Add an NS record in Vercel DNS (or your registrar):

      Name:   meridian
      Type:   NS
      Values:
${join("\n", formatlist("        %s", aws_route53_zone.main.name_servers))}

    This delegates ${var.domain} to Route 53
    while Vercel keeps handling your main site.

  EOT
}

output "postgres_connection" {
  description = "Postgres connection info (password is in var.db_password)"
  value       = "host=${aws_eip.virginia.public_ip} port=5432 dbname=meridian user=meridian"
}

# Sensitive outputs: `terraform output` shows "(sensitive value)".
# Scripts read them with `terraform output -raw db_password`.
output "db_password" {
  description = "Postgres password (for use by deploy scripts)"
  value       = var.db_password
  sensitive   = true
}

output "api_key" {
  description = "Project API key (for use by deploy scripts)"
  value       = var.api_key
  sensitive   = true
}

output "security_warning" {
  description = "Reminder about sensitive data in state"
  value       = "terraform.tfstate contains secrets (db_password, api_key). Never commit it. Consider S3 backend for teams."
}
