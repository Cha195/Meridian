# route53.tf — DNS zone and latency-based routing records.
#
# Route 53 is a global AWS service (no region needed — uses the default provider).
#
# Latency-based routing: 3 A records share the same name (proxy.meridian.sricharan.dev)
# but different set_identifiers and latency_routing_policy regions. When a user
# resolves the hostname, Route 53 returns the IP of whichever region has the
# lowest latency from that user's DNS resolver.

resource "aws_route53_zone" "main" {
  name = var.domain
}

# --- Virginia (us-east-1) ---
resource "aws_route53_record" "proxy_virginia" {
  zone_id = aws_route53_zone.main.zone_id
  name    = "proxy.${var.domain}"
  type    = "A"
  ttl     = 60

  set_identifier = "virginia"

  latency_routing_policy {
    region = "us-east-1"
  }

  records = [aws_eip.virginia.public_ip]
}

# --- Ireland (eu-west-1) ---
resource "aws_route53_record" "proxy_ireland" {
  zone_id = aws_route53_zone.main.zone_id
  name    = "proxy.${var.domain}"
  type    = "A"
  ttl     = 60

  set_identifier = "ireland"

  latency_routing_policy {
    region = "eu-west-1"
  }

  records = [aws_eip.ireland.public_ip]
}

# --- Tokyo (ap-northeast-1) ---
resource "aws_route53_record" "proxy_tokyo" {
  zone_id = aws_route53_zone.main.zone_id
  name    = "proxy.${var.domain}"
  type    = "A"
  ttl     = 60

  set_identifier = "tokyo"

  latency_routing_policy {
    region = "ap-northeast-1"
  }

  records = [aws_eip.tokyo.public_ip]
}
