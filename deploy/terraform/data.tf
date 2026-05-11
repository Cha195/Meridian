# data.tf — Look up the latest Ubuntu 22.04 LTS AMI in each region.
#
# AMI IDs are region-specific and go stale over time (Canonical publishes
# new images monthly). Using a data source ensures `terraform plan` always
# picks the latest available image instead of failing with "AMI not found".
#
# Owner 099720109477 is Canonical's official AWS account.

data "aws_ami" "virginia" {
  provider    = aws.virginia
  most_recent = true
  owners      = ["099720109477"]

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_ami" "ireland" {
  provider    = aws.ireland
  most_recent = true
  owners      = ["099720109477"]

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_ami" "tokyo" {
  provider    = aws.tokyo
  most_recent = true
  owners      = ["099720109477"]

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}
