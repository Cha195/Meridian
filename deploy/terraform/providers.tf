# providers.tf — One provider block per AWS region.
#
# Terraform talks to AWS one region at a time. To create resources in 3
# regions, we need 3 provider blocks with aliases. Each resource declares
# which provider it belongs to with `provider = aws.<alias>`.
#
# The default (unaliased) provider is us-east-1 — used by resources that
# don't specify a provider, like Route 53 (which is global).

provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "virginia"
  region = "us-east-1"
}

provider "aws" {
  alias  = "ireland"
  region = "eu-west-1"
}

provider "aws" {
  alias  = "tokyo"
  region = "ap-northeast-1"
}
