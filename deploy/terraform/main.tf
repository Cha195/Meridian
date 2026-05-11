# main.tf — EC2 instances, Elastic IPs, security groups, and SSH keys.
#
# Every resource type appears 3 times: once per region. This is the standard
# Terraform multi-region pattern — you cannot loop over provider aliases.
#
# Resource creation order (Terraform resolves this from references):
#   aws_eip → aws_security_group → aws_instance → aws_eip_association

# ---------------------------------------------------------------------------
# SSH Key Pairs — upload the same public key to all 3 regions
# ---------------------------------------------------------------------------

resource "aws_key_pair" "virginia" {
  provider   = aws.virginia
  key_name   = "meridian-key"
  public_key = file(var.ssh_public_key_path)
}

resource "aws_key_pair" "ireland" {
  provider   = aws.ireland
  key_name   = "meridian-key"
  public_key = file(var.ssh_public_key_path)
}

resource "aws_key_pair" "tokyo" {
  provider   = aws.tokyo
  key_name   = "meridian-key"
  public_key = file(var.ssh_public_key_path)
}

# ---------------------------------------------------------------------------
# Elastic IPs — static public IPs that persist across instance restarts.
# Created BEFORE instances so security groups can reference them.
# Free when attached to a running instance.
# ---------------------------------------------------------------------------

resource "aws_eip" "virginia" {
  provider = aws.virginia
  domain   = "vpc"
  tags     = { Name = "meridian-virginia" }
}

resource "aws_eip" "ireland" {
  provider = aws.ireland
  domain   = "vpc"
  tags     = { Name = "meridian-ireland" }
}

resource "aws_eip" "tokyo" {
  provider = aws.tokyo
  domain   = "vpc"
  tags     = { Name = "meridian-tokyo" }
}

# ---------------------------------------------------------------------------
# Security Groups
#
# Ports open:
#   22   — SSH (restricted to var.ssh_allowed_cidr)
#   80   — HTTP (public — for Let's Encrypt and redirects)
#   443  — HTTPS (public — main traffic entrypoint)
#   8080 — Meridian proxy (public — CDN traffic)
#   9090 — Admin API (restricted to the 3 node EIPs — has purge/migrate)
#   5432 — Postgres (us-east-1 only, restricted to the 3 node EIPs)
# ---------------------------------------------------------------------------

locals {
  # All 3 Elastic IPs as /32 CIDRs for security group rules.
  peer_cidrs = [
    "${aws_eip.virginia.public_ip}/32",
    "${aws_eip.ireland.public_ip}/32",
    "${aws_eip.tokyo.public_ip}/32",
  ]
}

resource "aws_security_group" "virginia" {
  provider    = aws.virginia
  name        = "meridian-virginia"
  description = "Meridian edge node — Virginia (primary, runs Postgres)"

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.ssh_allowed_cidr]
  }

  ingress {
    description      = "HTTP"
    from_port        = 80
    to_port          = 80
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description      = "HTTPS"
    from_port        = 443
    to_port          = 443
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description      = "Meridian proxy"
    from_port        = 8080
    to_port          = 8080
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description = "Admin API — restricted to cluster nodes"
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = local.peer_cidrs
  }

  ingress {
    description = "Postgres — restricted to cluster nodes"
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = local.peer_cidrs
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "meridian-virginia" }
}

resource "aws_security_group" "ireland" {
  provider    = aws.ireland
  name        = "meridian-ireland"
  description = "Meridian edge node — Ireland"

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.ssh_allowed_cidr]
  }

  ingress {
    description      = "HTTP"
    from_port        = 80
    to_port          = 80
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description      = "HTTPS"
    from_port        = 443
    to_port          = 443
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description      = "Meridian proxy"
    from_port        = 8080
    to_port          = 8080
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description = "Admin API — restricted to cluster nodes"
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = local.peer_cidrs
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "meridian-ireland" }
}

resource "aws_security_group" "tokyo" {
  provider    = aws.tokyo
  name        = "meridian-tokyo"
  description = "Meridian edge node — Tokyo"

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.ssh_allowed_cidr]
  }

  ingress {
    description      = "HTTP"
    from_port        = 80
    to_port          = 80
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description      = "HTTPS"
    from_port        = 443
    to_port          = 443
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description      = "Meridian proxy"
    from_port        = 8080
    to_port          = 8080
    protocol         = "tcp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  ingress {
    description = "Admin API — restricted to cluster nodes"
    from_port   = 9090
    to_port     = 9090
    protocol    = "tcp"
    cidr_blocks = local.peer_cidrs
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "meridian-tokyo" }
}

# ---------------------------------------------------------------------------
# EC2 Instances — one per region, running Ubuntu 22.04 LTS.
# The user_data script runs once on first boot to install dependencies.
#
# WARNING: changing user_data (including db_password or maxmind_license_key)
# triggers instance RECREATION — Terraform destroys the old instance and
# creates a new one. On Virginia this means losing the Postgres data.
# To change secrets on a running instance, SSH in and update manually.
# ---------------------------------------------------------------------------

resource "aws_instance" "virginia" {
  provider               = aws.virginia
  ami                    = data.aws_ami.virginia.id
  instance_type          = var.instance_type
  key_name               = aws_key_pair.virginia.key_name
  vpc_security_group_ids = [aws_security_group.virginia.id]

  root_block_device {
    volume_size = 8
    volume_type = "gp3"
  }

  user_data = templatefile("${path.module}/templates/user_data.sh.tpl", {
    region              = "us-east-1"
    node_name           = "virginia"
    is_primary          = true
    db_password         = var.db_password
    maxmind_license_key = var.maxmind_license_key
    peer_ips            = [aws_eip.virginia.public_ip, aws_eip.ireland.public_ip, aws_eip.tokyo.public_ip]
  })

  tags = { Name = "meridian-virginia" }
}

resource "aws_instance" "ireland" {
  provider               = aws.ireland
  ami                    = data.aws_ami.ireland.id
  instance_type          = var.instance_type
  key_name               = aws_key_pair.ireland.key_name
  vpc_security_group_ids = [aws_security_group.ireland.id]

  root_block_device {
    volume_size = 8
    volume_type = "gp3"
  }

  user_data = templatefile("${path.module}/templates/user_data.sh.tpl", {
    region              = "eu-west-1"
    node_name           = "ireland"
    is_primary          = false
    db_password         = var.db_password
    maxmind_license_key = var.maxmind_license_key
    peer_ips            = [aws_eip.virginia.public_ip, aws_eip.ireland.public_ip, aws_eip.tokyo.public_ip]
  })

  tags = { Name = "meridian-ireland" }
}

resource "aws_instance" "tokyo" {
  provider               = aws.tokyo
  ami                    = data.aws_ami.tokyo.id
  instance_type          = var.instance_type
  key_name               = aws_key_pair.tokyo.key_name
  vpc_security_group_ids = [aws_security_group.tokyo.id]

  root_block_device {
    volume_size = 8
    volume_type = "gp3"
  }

  user_data = templatefile("${path.module}/templates/user_data.sh.tpl", {
    region              = "ap-northeast-1"
    node_name           = "tokyo"
    is_primary          = false
    db_password         = var.db_password
    maxmind_license_key = var.maxmind_license_key
    peer_ips            = [aws_eip.virginia.public_ip, aws_eip.ireland.public_ip, aws_eip.tokyo.public_ip]
  })

  tags = { Name = "meridian-tokyo" }
}

# ---------------------------------------------------------------------------
# EIP Associations — link static IPs to instances.
# Separate from aws_eip so the EIP exists before the instance (SGs need it).
# ---------------------------------------------------------------------------

resource "aws_eip_association" "virginia" {
  provider      = aws.virginia
  instance_id   = aws_instance.virginia.id
  allocation_id = aws_eip.virginia.id
}

resource "aws_eip_association" "ireland" {
  provider      = aws.ireland
  instance_id   = aws_instance.ireland.id
  allocation_id = aws_eip.ireland.id
}

resource "aws_eip_association" "tokyo" {
  provider      = aws.tokyo
  instance_id   = aws_instance.tokyo.id
  allocation_id = aws_eip.tokyo.id
}
