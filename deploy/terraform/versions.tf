# versions.tf — Pin Terraform and provider versions.
# `terraform init` downloads the AWS provider plugin on first run.

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}
