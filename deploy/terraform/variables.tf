# variables.tf — All configurable inputs.
# Set values in terraform.tfvars (gitignored) or pass via -var flags.

variable "domain" {
  description = "Domain for Route 53 zone (e.g. meridian.sricharan.dev)"
  type        = string
  default     = "meridian.sricharan.dev"
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key to upload to each instance"
  type        = string
  default     = "~/.ssh/id_rsa.pub"
}

variable "instance_type" {
  description = "EC2 instance type — t3.micro (1 vCPU, 1GB) is enough for a side project. Upgrade to t3.small if you need more cache memory"
  type        = string
  default     = "t3.micro"
}

variable "ssh_allowed_cidr" {
  description = "CIDR block allowed to SSH. Use YOUR_IP/32 in production. Run: curl -s ifconfig.me"
  type        = string
  default     = "0.0.0.0/0"
}

variable "db_password" {
  description = "Password for the 'meridian' Postgres user on us-east-1"
  type        = string
  sensitive   = true
}

variable "api_key" {
  description = "API key for the default project. Used in meridian.yaml for authenticated cache/migration endpoints"
  type        = string
  sensitive   = true
}

variable "maxmind_license_key" {
  description = "MaxMind GeoLite2 license key for GeoIP database. Empty string = skip GeoIP install. Sign up free at maxmind.com"
  type        = string
  default     = ""
  sensitive   = true
}
