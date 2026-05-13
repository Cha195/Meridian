# Deployment Architecture

This document explains every file in the `deploy/` directory, the services
involved, and the reasoning behind each design decision.

## What gets deployed

```
                          ┌─────────────────┐
                          │    Route 53      │
                          │  Latency-based   │
                          │   DNS routing    │
                          └────┬───┬───┬─────┘
                               │   │   │
               ┌───────────────┘   │   └───────────────┐
               ▼                   ▼                   ▼
      ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
      │   us-east-1      │ │   eu-west-1      │ │ ap-northeast-1   │
      │   (Virginia)     │ │   (Ireland)      │ │   (Tokyo)        │
      │                  │ │                  │ │                  │
      │  Meridian proxy  │ │  Meridian proxy  │ │  Meridian proxy  │
      │  Admin API :9090 │ │  Admin API :9090 │ │  Admin API :9090 │
      │  Postgres        │ │                  │ │                  │
      │  (primary only)  │ │                  │ │                  │
      └──────────────────┘ └──────────────────┘ └──────────────────┘
```

Three identical EC2 instances, each running the same Meridian binary. The only
difference is Virginia (us-east-1) also runs Postgres as the central analytics
store. Ireland and Tokyo send events to Virginia's Postgres over the internet.

## Event pipeline

```
Proxy request arrives
  → proxy.ServeHTTP emits a DiagnosticEvent
  → Emitter.Emit(event) — non-blocking channel send (drops if channel full)
  → Emitter worker goroutine reads from the channel
  → StdoutOutput.Write(event) — JSON line to stdout (for local debugging)
  → PostgresStore.Write(event) — appends to an in-memory buffer
  → When buffer hits 100 events OR 5 seconds pass (whichever first):
      → pgx.CopyFrom batch-inserts all buffered events in one round trip
      → Buffer is cleared
```

**Why batching:** inserting events one-at-a-time means one Postgres round trip
per request. At 1000 req/s, that's 1000 round trips/s. Batching 100 events
into one INSERT means 10 round trips/s — 100x fewer. The `CopyFrom` method
uses Postgres's COPY protocol, which is even faster than multi-row INSERT.

**Why a flush ticker:** if traffic is low (say, 5 requests/minute), the buffer
never hits 100 events. Without the ticker, those events would sit in memory
indefinitely. The 5-second ticker ensures events reach Postgres within 5s
even under low traffic.

**Why a mutex:** two goroutines touch the buffer — the Emitter's worker
(calling `Write`) and the flush ticker (calling `Flush`). The mutex prevents
them from appending and flushing simultaneously.

**Failure mode:** if Postgres is unreachable (network blip, maintenance), the
CopyFrom call fails. The events in that batch are logged and dropped. The
proxy never blocks — `Emitter.Emit` is non-blocking (drops on channel full)
and `PostgresStore.Write` just appends to the buffer (fast). User-facing
latency is never affected by database issues.

## How the API server is wired together

The API server (`internal/api/server.go`) is assembled in `cmd/meridian/main.go`
using functional options. Each option injects an optional dependency:

```go
apiServer := api.NewAPIServer(cfg, emitter, clusterState, geoLocator, caches,
    api.WithWebSocketBroadcaster(wsBroadcaster),
    api.WithDashboardFS(dashFS),
    api.WithDB(dbPool),
)
```

**Why functional options instead of a big constructor?** Not every dependency
exists in every environment. If Postgres is down at startup, `dbPool` is nil.
If the dashboard files aren't embedded, `dashFS` is nil. The API server works
without any of these — it just skips the routes that need them. The option
pattern lets the caller wire up only what's available.

### The three optional dependencies

**`dbPool` (`*pgxpool.Pool`)** — a connection pool to Postgres. Created by
`PostgresStore` when the database config is present. Shared between the
PostgresStore (which uses it for batch event inserts) and the API server
(which uses it for dashboard query endpoints like `/api/v1/stats/live`).

`pgxpool` manages a pool of connections internally — it opens connections on
demand, reuses them, and caps the total. You don't open/close connections
per query. Every `pool.Query()` or `pool.QueryRow()` borrows a connection,
runs the query, and returns it to the pool. If all connections are in use,
new queries wait. This is standard database connection pooling.

If `dbPool` is nil (Postgres not configured), all dashboard endpoints return
503 Service Unavailable. The proxy still works — it just can't show analytics.

**`wsBroadcaster` (`*WebSocketBroadcaster`)** — pushes live events to connected
dashboard clients over WebSocket. It implements the `EventOutput` interface,
so the emitter writes events to it alongside stdout and Postgres.

How it works internally:

```
Emitter worker goroutine
  → wsBroadcaster.Write(event)
  → marshal event to JSON (once, shared across all clients)
  → for each connected client:
      → try to send JSON to client's channel (buffered, size 64)
      → if channel is full (slow client): drop the event for that client
  → return immediately (never blocks the emitter)

Per-client write goroutine (one per WebSocket connection):
  → reads from the client's channel
  → writes to the WebSocket connection
  → if write fails (client disconnected): removes client, closes connection
```

The key design: the emitter's `Write()` call is O(N) where N is the number
of connected clients, but each client gets a buffered channel. If a client
can't keep up (slow network, suspended browser tab), events are dropped for
THAT client only — other clients and the emitter are unaffected. This is the
"per-client channel with select/default drop" pattern.

Authentication for WebSocket uses a query parameter (`?key=API_KEY`) instead
of the `Authorization` header, because the browser's `WebSocket` API doesn't
support custom headers.

**`dashFS` (`fs.FS`)** — the dashboard's HTML, CSS, and JS files, embedded
into the Go binary via `//go:embed dashboard/*` in `dashboard.go` at the
project root.

```go
//go:embed dashboard/*
var DashboardFS embed.FS
```

At startup, `fs.Sub(meridian.DashboardFS, "dashboard")` strips the `dashboard/`
prefix so `index.html` is served at `/` instead of `/dashboard/index.html`.

The embed directive means the dashboard ships inside the binary — no separate
files to deploy, no directory to SCP. `deploy.sh` pushes one file and the
dashboard is included. If the dashboard files are missing at compile time
(e.g., someone deleted the directory), the embed fails at build time, not at
runtime.

### How events flow through all three outputs

```
Proxy request
  → proxy.ServeHTTP emits DiagnosticEvent
  → emitter.Emit(event)  [non-blocking channel send]
  → emitter worker goroutine reads event
  → StdoutOutput.Write(event)         → JSON line to stdout
  → PostgresStore.Write(event)        → append to buffer → batch CopyFrom to Postgres
  → WebSocketBroadcaster.Write(event) → fan out to connected dashboard clients

All three happen sequentially in the emitter's single worker goroutine.
If any Write is slow, it delays the others. That's why:
  - StdoutOutput is a simple Encode (fast, ~microseconds)
  - PostgresStore just appends to a buffer (fast, ~microseconds)
    and does the actual CopyFrom in a separate timer goroutine
  - WebSocketBroadcaster just sends to channels (fast, ~microseconds)
    and each client has its own write goroutine for the actual WebSocket send
```

None of the three outputs block on network I/O in their `Write()` method.
The network calls (Postgres CopyFrom, WebSocket write) happen in background
goroutines. This ensures the emitter's worker processes events at memory speed.

## How users reach the right server

When a user's browser resolves `proxy.meridian.sricharan.dev`, the request goes
to Route 53. Route 53 has three A records for that hostname, each tagged with
a different AWS region. It checks which region has the lowest network latency
from the user's DNS resolver and returns that region's Elastic IP.

This happens entirely at the DNS layer — before any HTTP request is made. The
browser gets an IP, connects to it, and the Meridian proxy handles the request.

**Why does Meridian also have GeoIP + ClosestNode if Route 53 handles routing?**

They solve different problems:
- **Route 53** decides which server the user reaches (DNS, before the request)
- **Meridian's ClosestNode** checks whether Route 53 picked the *right* server
  (application layer, after the request arrives)

The `routing_correct` field in each diagnostic event tells you "was this user
served by their nearest node?" Route 53 gets it right ~90% of the time, but
DNS resolver geography can cause mismatches — a user in Poland whose ISP routes
DNS queries through a US-based resolver will get the Virginia IP instead of
Ireland. Meridian detects and logs these misroutes for your analytics dashboard.

## Directory structure

```
deploy/
├── terraform/                  # Infrastructure-as-Code (Terraform)
│   ├── versions.tf             # Terraform + provider version pins
│   ├── providers.tf            # AWS provider aliases (one per region)
│   ├── variables.tf            # All configurable inputs
│   ├── data.tf                 # Dynamic AMI lookups (Ubuntu 22.04)
│   ├── main.tf                 # EC2 instances, EIPs, security groups, SSH keys
│   ├── route53.tf              # DNS zone + latency-routed A records
│   ├── outputs.tf              # IPs, SSH commands, NS records, warnings
│   ├── terraform.tfvars.example # Template for secrets (copy to terraform.tfvars)
│   └── templates/
│       └── user_data.sh.tpl    # Instance bootstrap script (runs on first boot)
├── systemd/
│   └── meridian.service        # Systemd unit file for the Meridian process
├── scripts/
│   ├── deploy.sh               # Build + SCP + restart (replaces CI/CD)
│   ├── setup-postgres.sh       # Initialize Postgres schema on Virginia
│   ├── generate-config.sh      # Generate meridian.yaml for a given node
│   ├── status.sh               # Check health across all nodes
│   └── stop-all.sh             # Stop service / tear down infrastructure
└── configs/
    └── .gitkeep                # Generated YAML configs go here (gitignored)
```

## How Route 53 actually works

DNS (Domain Name System) translates hostnames into IP addresses. When you type
`proxy.meridian.sricharan.dev` in a browser, your computer doesn't know what
IP that is. It asks a DNS resolver (usually your ISP's, or 8.8.8.8 if you
use Google's). The resolver walks the DNS tree to find the answer:

```
Browser: "What's the IP for proxy.meridian.sricharan.dev?"
   │
   ▼
Resolver → root DNS servers: "Who handles .dev?"
         → .dev servers: "Who handles sricharan.dev?"
         → Vercel DNS (your registrar): "Who handles meridian.sricharan.dev?"
         → Vercel sees your NS delegation record, responds:
           "Ask Route 53: ns-xxx.awsdns-xx.com"
         → Route 53: "The IP is 3.92.xxx.xxx" (the Virginia Elastic IP)
   │
   ▼
Browser connects to 3.92.xxx.xxx → Meridian proxy handles the request
```

### What makes Route 53 special: latency-based routing

Normal DNS returns the same IP for every user. Route 53 can return
**different** IPs depending on who's asking.

We create 3 A records with the same name (`proxy.meridian.sricharan.dev`):

| Record | IP points to | Tagged region |
|---|---|---|
| A record (set: virginia) | Virginia Elastic IP | us-east-1 |
| A record (set: ireland) | Ireland Elastic IP | eu-west-1 |
| A record (set: tokyo) | Tokyo Elastic IP | ap-northeast-1 |

When a DNS resolver asks Route 53 for this hostname, Route 53 checks: "which
of my 3 regions has the lowest network latency from this resolver's IP?" and
returns only that region's IP.

- A resolver in Frankfurt → Route 53 returns Ireland's IP (closest)
- A resolver in California → Route 53 returns Virginia's IP
- A resolver in Seoul → Route 53 returns Tokyo's IP

The user's browser only ever sees one IP. It has no idea there are 3 servers.

### How Route 53 knows the latency

Route 53 doesn't measure latency in real-time. AWS maintains a global database
of approximate latency from every network block to every AWS region, built from
years of network measurements across their infrastructure. When a query comes
from resolver IP 203.0.113.50, Route 53 looks up that IP in the database and
picks the lowest-latency region.

This is why it's ~90% accurate, not 100%. The database maps resolver IPs to
regions, but your resolver's IP might not reflect your actual location (e.g.,
a VPN, a corporate DNS resolver in a different country, or a cloud-based
resolver like 8.8.8.8 that has anycast nodes everywhere).

### The NS delegation step (why you need to add records to Vercel)

You own `sricharan.dev` through Vercel. Vercel's DNS servers handle all lookups
for `*.sricharan.dev`. But Route 53 needs to handle `*.meridian.sricharan.dev`
because Vercel DNS doesn't support latency-based routing.

The NS delegation record tells Vercel: "for anything under
`meridian.sricharan.dev`, don't answer yourself — forward the query to these
Route 53 nameservers instead." Vercel keeps handling your main site, blog,
etc. Only the `meridian` subdomain goes to Route 53.

After `terraform apply`, the output shows the 4 Route 53 nameservers. You add
one NS record in Vercel:

```
Name:  meridian
Type:  NS
Value: ns-123.awsdns-12.com
       ns-456.awsdns-34.net
       ns-789.awsdns-56.co.uk
       ns-012.awsdns-78.org
```

### TTL and what happens when a node goes down

Each DNS record has a TTL (Time To Live) — how long resolvers should cache the
answer before asking again. We set TTL to 60 seconds.

If the Tokyo node goes down:
1. Users who already cached Tokyo's IP keep trying it for up to 60 seconds
2. After 60 seconds, their cache expires and they re-resolve the hostname
3. Route 53 sees Tokyo is unresponsive (if Route 53 health checks are
   configured) and returns Ireland or Virginia instead
4. The user gets rerouted to a working node

Lower TTL = faster failover but slightly more DNS traffic. 60 seconds is a
good balance for a 3-node CDN.

## Why these decisions? (DevOps reasoning)

### Why Terraform instead of clicking around the AWS console?

The AWS console lets you create EC2 instances, security groups, DNS records
etc. by clicking through web forms. The problem: if you need to recreate
everything (you messed up, you want a second environment, you're showing
someone else how), you have to remember every click. Terraform describes your
infrastructure in text files. `terraform apply` creates everything.
`terraform destroy` deletes everything. If you mess up, delete and start over.
The files ARE the documentation of what exists.

### Why 3 separate providers instead of one?

AWS is organized into regions — each region is a physically separate data
center cluster (Virginia, Ireland, Tokyo). Each region has its own set of
AMIs, its own security groups, its own EC2 instances. They don't share
anything. When you call the AWS API to create an instance, you're calling
a specific region's API endpoint.

Terraform's `provider "aws"` block is essentially "which region's API should
I talk to?" Since we need to create resources in 3 different regions, we need
3 different provider blocks, each configured with a different region:

```hcl
provider "aws" { alias = "virginia"; region = "us-east-1" }
provider "aws" { alias = "ireland";  region = "eu-west-1" }
provider "aws" { alias = "tokyo";    region = "ap-northeast-1" }
```

### Why is there also an unaliased default provider?

Some AWS services are **global** — they don't belong to any single region.
Route 53 (DNS) is the main one. When you create a Route 53 hosted zone, it
doesn't live in us-east-1 or eu-west-1 — it lives in AWS's global
infrastructure. But Terraform still requires a provider for every resource.

If a resource doesn't have `provider = aws.something`, Terraform uses the
**default provider** — the one with no `alias`. We set the default to
us-east-1 because AWS convention is that global services are accessed via
the us-east-1 API endpoint (even though they're not actually "in" that
region). Route 53 resources use this default provider.

Without the default, every Route 53 resource would need an explicit
`provider = aws.virginia`, which works but is misleading — it implies Route 53
is in Virginia when it's actually global.

### Why Elastic IPs?

When you create an EC2 instance, AWS assigns it a random public IP address.
If you reboot the instance (or stop and start it to save money), AWS gives it
a **different** random IP. Your Route 53 DNS records still point to the old
IP. Traffic goes nowhere until you manually update the DNS records.

An Elastic IP is a static IP that you own. It persists across reboots. You
associate it with an instance, and if you stop/start the instance, the same IP
stays attached. Your DNS records always point to the right place.

They're free while attached to a running instance. They cost ~$3.60/month if
the instance is stopped (AWS charges for "wasting" a public IPv4 address).

### Why look up AMIs dynamically instead of hardcoding them?

An AMI (Amazon Machine Image) is a snapshot of an operating system. To launch
an Ubuntu instance, you need the AMI ID for Ubuntu 22.04 in that specific
region. AMI IDs look like `ami-0c7217cdde317cfec` — opaque strings that are
different in every region and change when Canonical publishes updates.

If you hardcode an AMI ID in your Terraform, two things happen:
1. Someone in a different region can't use your code (wrong AMI ID)
2. After a few months, Canonical delists the old image and `terraform apply`
   fails with "AMI not found"

The `aws_ami` data source queries AWS at plan time and finds the latest
matching image. It will always work, in any region, at any point in time.

### Why restrict port 9090 to cluster IPs only?

Port 9090 is Meridian's admin API. It exposes endpoints like:
- `POST /api/v1/cache/purge` — delete cached content
- `POST /api/v1/cache/migrate` — start a live cache policy migration
- `POST /api/v1/cache/migrate/abort` — cancel a migration

If this port were public, anyone who discovers it could purge your cache
(causing a storm of origin requests) or start/abort migrations (destabilizing
your cache). Even though these endpoints require an API key, defense in depth
says: don't expose the port at all unless necessary.

The only things that need to reach port 9090:
- The other 2 cluster nodes (for health checks — they probe `/healthz`)
- You, via SSH tunnel (`ssh -L 9090:localhost:9090 ubuntu@<ip>`)

So the security group only allows traffic from the 3 Elastic IPs.

### Why Postgres on an EC2 instance instead of RDS?

AWS RDS (Relational Database Service) is a managed Postgres service — AWS
handles backups, patching, failover, etc. It's the "right" choice for
production. But:

- RDS minimum cost is ~$15/month for a single-AZ db.t3.micro
- You can't SSH into it to debug
- It adds another service to understand and configure
- For a side project that might be stopped/destroyed frequently, the
  simplicity of "Postgres is just apt-get installed on the Virginia box"
  wins over the operational benefits of RDS

If this project grows, migrating to RDS is straightforward — change the
connection string and run the schema migration.

### Why Redis on every node?

Redis serves as a local event buffer. When Meridian processes a request, it
generates a `DiagnosticEvent`. That event needs to reach Postgres in Virginia.
But writing to a remote database on every request adds latency and creates a
failure point — if Virginia's Postgres is slow or unreachable, request
processing shouldn't block.

The pattern (implemented in Stage 9): write events to local Redis immediately
(fast, ~0.1ms), then a background worker drains Redis and batch-inserts into
Postgres. If Postgres is down, events accumulate in Redis and drain when it
recovers. No events are lost, and request latency is unaffected.

### Why a deploy script instead of CI/CD?

CI/CD (GitHub Actions, etc.) triggers automatically on every push: build the
binary, run tests, deploy to servers. It's the standard for teams, but for a
solo side project:

- GitHub Actions is another service to configure and debug
- If your EC2s are stopped (cost savings), the deploy step fails
- You don't push code frequently enough to justify automated deploys
- A script gives you explicit control: "deploy now, to this node"

`deploy.sh` does everything CI/CD would: cross-compile, SCP to servers,
restart the service. The difference is you run it manually instead of it
running on every push.

### Why systemd?

systemd is the init system on Ubuntu (and most modern Linux). It manages
long-running processes — starting them on boot, restarting them on crash,
capturing their logs. Without systemd, you'd have to:

- Start Meridian manually after each reboot
- Write your own crash-restart logic
- Set up log rotation yourself

The `meridian.service` file tells systemd: "run this binary as the `meridian`
user, restart it if it crashes, and start it after the network is up." Then
`systemctl start/stop/status meridian` manages the process, and
`journalctl -u meridian` shows its logs.

### Why a separate `meridian` system user?

The Meridian binary runs as the `meridian` user, not as `root`. If the binary
has a security vulnerability (e.g., a path traversal bug), the attacker can
only access files owned by `meridian` — not the entire system. This is the
principle of least privilege.

The `meridian` user has `--shell /usr/sbin/nologin` — you can't SSH in as
this user. It exists only to own the process and its files.

### Why is setup-postgres.sh separate from user data?

The user data script runs once, on first boot, in a fire-and-forget manner.
You can't easily re-run it, and its output is buried in cloud-init logs.

The Postgres schema, on the other hand, will change as you add features
(new tables, new indexes, column additions). You want to run schema migrations
explicitly, see the output, and know they succeeded. A separate script that
you run from your laptop via SSH gives you that control.

The user data script installs Postgres and creates the database/user. The
setup script runs the actual schema migration. This separation means you
can update the schema without reprovisioning the instance.

## Reading Terraform syntax (with examples from our code)

If you've never read Terraform before, here's how to parse it. Every example
below is from our actual files.

### Blocks: the building unit

Everything in Terraform is a **block** — a keyword, optional labels, and a
body in braces:

```hcl
keyword "type" "name" {
  argument = value
}
```

The three kinds of blocks you'll see:

```hcl
# 1. resource — something Terraform creates in AWS
resource "aws_instance" "virginia" { ... }
#         ↑ AWS type       ↑ your name for it (used to reference it elsewhere)

# 2. data — something that already exists, Terraform just reads it
data "aws_ami" "virginia" { ... }
#     ↑ type     ↑ name

# 3. variable — an input you provide
variable "db_password" { ... }
#          ↑ name
```

### How references work

When one resource needs a value from another, you reference it by
`<type>.<name>.<attribute>`. Terraform uses these references to build its
dependency graph — it knows what to create first.

From our `main.tf`:

```hcl
resource "aws_eip" "virginia" {     # Creates an Elastic IP
  provider = aws.virginia
  domain   = "vpc"
}

resource "aws_security_group" "virginia" {
  ingress {
    cidr_blocks = ["${aws_eip.virginia.public_ip}/32"]
    #                  ↑ reference to the EIP above
    #                  Terraform reads this and knows: "I need to create
    #                  the EIP before the security group"
  }
}
```

The string `"${aws_eip.virginia.public_ip}/32"` is string interpolation —
the `${}` part gets replaced with the EIP's actual IP address at apply time.

### The `locals` block

`locals` defines computed values you reuse. Think of it as a variable you
set yourself (unlike `variable`, which the user provides):

```hcl
locals {
  peer_cidrs = [
    "${aws_eip.virginia.public_ip}/32",
    "${aws_eip.ireland.public_ip}/32",
    "${aws_eip.tokyo.public_ip}/32",
  ]
}

# Then used later:
resource "aws_security_group" "virginia" {
  ingress {
    from_port   = 9090
    cidr_blocks = local.peer_cidrs    # all 3 IPs
  }
}
```

Without `locals`, we'd repeat the same 3-element list in every security group.

### Security group ingress rules

The selection you highlighted:

```hcl
ingress {
  description = "SSH"
  from_port   = 22        # allow port 22...
  to_port     = 22        # ...to port 22 (single port, so from == to)
  protocol    = "tcp"     # TCP only (SSH uses TCP)
  cidr_blocks = [var.ssh_allowed_cidr]  # ...from these IP ranges
}
```

`cidr_blocks` controls WHO can connect. CIDR notation works like this:
- `"0.0.0.0/0"` = every IP address on the internet (wide open)
- `"73.162.99.42/32"` = exactly one IP (the `/32` means "all 32 bits must match")
- `"10.0.0.0/8"` = any IP starting with 10.x.x.x

So `cidr_blocks = [var.ssh_allowed_cidr]` means "allow SSH from whatever IP
range the user specified." Default is `0.0.0.0/0` (open), but you should set
it to `YOUR_IP/32` in production.

Compare with the admin API rule:

```hcl
ingress {
  description = "Admin API — restricted to cluster nodes"
  from_port   = 9090
  to_port     = 9090
  protocol    = "tcp"
  cidr_blocks = local.peer_cidrs  # only the 3 node IPs, not the whole internet
}
```

Same structure, but `cidr_blocks` is the 3 Elastic IPs instead of `0.0.0.0/0`.

The egress (outbound) rule is wide open — the instances can talk to anything:

```hcl
egress {
  from_port   = 0           # all ports...
  to_port     = 0
  protocol    = "-1"        # "-1" means all protocols (TCP, UDP, ICMP, etc.)
  cidr_blocks = ["0.0.0.0/0"]  # ...to anywhere
}
```

### The `provider` argument

Every resource needs to know which AWS region to create in. That's what
`provider` does:

```hcl
resource "aws_instance" "ireland" {
  provider      = aws.ireland        # create this in eu-west-1
  ami           = data.aws_ami.ireland.id  # use Ireland's AMI
  instance_type = var.instance_type
  key_name      = aws_key_pair.ireland.key_name  # use Ireland's SSH key
  ...
}
```

If you omit `provider`, Terraform uses the default (unaliased) provider.

### The `data` block (reading, not creating)

Data sources **read** existing information from AWS. They don't create
anything. Our AMI lookup:

```hcl
data "aws_ami" "virginia" {
  provider    = aws.virginia       # look in us-east-1
  most_recent = true               # if multiple match, pick the newest
  owners      = ["099720109477"]   # only AMIs published by Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
    #         ↑ wildcard — matches any date suffix
  }
}
```

This says: "In us-east-1, find the most recent AMI whose name matches
`ubuntu-jammy-22.04-*` and is owned by Canonical." The result is referenced
as `data.aws_ami.virginia.id` — the AMI ID string that `aws_instance` needs.

### `templatefile()` — injecting values into scripts

EC2 user data is a bash script, but it needs values that only exist at apply
time (the DB password, the peer IPs). `templatefile()` reads a `.tpl` file
and substitutes variables:

```hcl
resource "aws_instance" "virginia" {
  user_data = templatefile("${path.module}/templates/user_data.sh.tpl", {
    region              = "us-east-1"
    node_name           = "virginia"
    is_primary          = true           # only Virginia runs Postgres
    db_password         = var.db_password
    peer_ips            = [aws_eip.virginia.public_ip, aws_eip.ireland.public_ip, ...]
  })
}
```

Inside the `.tpl` file, you use `${variable}` for substitution and
`%{ if ... }` / `%{ for ... }` for control flow:

```bash
# Simple substitution
echo "Setting up ${node_name} in ${region}"

# Conditional (only on Virginia)
%{ if is_primary }
apt-get install -y postgresql
%{ endif }

# Loop (add each peer IP to pg_hba.conf)
%{ for ip in peer_ips }
echo "host meridian meridian ${ip}/32 scram-sha-256" >> pg_hba.conf
%{ endfor }
```

`${path.module}` is a built-in that means "the directory this .tf file is
in" — so it finds `templates/user_data.sh.tpl` relative to `main.tf`.

### Route 53 latency routing records

Three records with the same hostname but different `set_identifier` values:

```hcl
resource "aws_route53_record" "proxy_virginia" {
  zone_id = aws_route53_zone.main.zone_id   # which DNS zone
  name    = "proxy.${var.domain}"            # the hostname
  type    = "A"                              # A record = hostname → IPv4
  ttl     = 60                               # cache for 60 seconds

  set_identifier = "virginia"  # unique label (Route 53 needs this to
                                # tell the 3 records apart)

  latency_routing_policy {
    region = "us-east-1"       # Route 53 returns this record when the
  }                            # querier has lowest latency to us-east-1

  records = [aws_eip.virginia.public_ip]  # the IP to return
}
```

The key insight: `name` is the same on all 3 records. Route 53 picks which
one to return based on `latency_routing_policy`. Without it, you'd have 3
records and DNS would round-robin between them randomly.

### Outputs — what you see after `terraform apply`

```hcl
output "ssh_commands" {
  description = "SSH into each instance"
  value = {
    virginia = "ssh -i ~/.ssh/id_rsa ubuntu@${aws_eip.virginia.public_ip}"
    ireland  = "ssh -i ~/.ssh/id_rsa ubuntu@${aws_eip.ireland.public_ip}"
    tokyo    = "ssh -i ~/.ssh/id_rsa ubuntu@${aws_eip.tokyo.public_ip}"
  }
}
```

After `terraform apply` finishes, it prints these values. You can also
retrieve them later with `terraform output ssh_commands`. The deploy scripts
use `terraform output -json` to read IPs programmatically.

### `file()` — reading a local file

```hcl
resource "aws_key_pair" "virginia" {
  public_key = file(var.ssh_public_key_path)
  #            ↑ reads ~/.ssh/id_rsa.pub from your laptop and uploads it
}
```

`file()` reads a file from disk at plan time. This is how your SSH public
key gets uploaded to AWS without pasting it into the Terraform config.

## Terraform files explained

### versions.tf

Pins Terraform to >= 1.5 and the AWS provider to ~> 5.0. This prevents
breaking changes from new major versions. `terraform init` downloads the
provider plugin based on these constraints.

### providers.tf

Three aliased providers (one per region) plus an unaliased default for global
services like Route 53. See "Why 3 separate providers?" and "Why is there
also an unaliased default provider?" above for the full reasoning.

### variables.tf

Six inputs:

| Variable | Why it exists |
|---|---|
| `domain` | The Route 53 hosted zone name |
| `ssh_public_key_path` | Uploaded to all 3 regions so you can SSH in |
| `instance_type` | t3.small (2 vCPU, 2GB) is enough for cache + proxy workloads |
| `ssh_allowed_cidr` | Defaults to `0.0.0.0/0` (open) — set to `YOUR_IP/32` in production |
| `db_password` | Postgres password. Marked `sensitive = true` so it's redacted in logs |
| `maxmind_license_key` | For downloading GeoLite2-City.mmdb. Empty string = skip GeoIP install |

Values go in `terraform.tfvars` (gitignored). Never commit secrets.

### data.tf

Looks up the latest Ubuntu 22.04 LTS AMI in each region. AMI IDs are
region-specific and Canonical publishes new images monthly. Hardcoding an AMI
ID means `terraform apply` breaks when that image is delisted. The data source
always picks the latest available one.

The filter `owners = ["099720109477"]` restricts to Canonical's official AWS
account — this prevents anyone from publishing a malicious AMI with a matching
name.

### main.tf

The biggest file. Creates these resources per region (3 copies of each):

**SSH key pairs** — the same public key uploaded to all 3 regions. EC2 key
pairs are region-scoped, so the same key must be registered separately in
each region.

**Elastic IPs** — static public IPs that persist across instance restarts.
Without these, rebooting an instance changes its IP and breaks DNS records.
Elastic IPs are free when attached to a running instance.

They're created *before* instances because security groups need to reference
them. This is the key ordering trick:

```
aws_eip (exists before anything else — no instance needed)
  ↓ referenced by
aws_security_group (allows port 9090/5432 from these specific IPs)
  ↓ referenced by
aws_instance (attached to this security group)
  ↓ referenced by
aws_eip_association (links the EIP to the instance)
```

Terraform resolves this dependency chain automatically from the references.
You don't write "create A then B" — you reference A inside B and Terraform
figures out the order.

**Security groups** — firewall rules for each instance:

| Port | Open to | Why |
|---|---|---|
| 22 | `ssh_allowed_cidr` | SSH access. Restrict in production |
| 80 | `0.0.0.0/0` | Let's Encrypt validation + HTTP→HTTPS redirects |
| 443 | `0.0.0.0/0` | Main HTTPS traffic |
| 8080 | `0.0.0.0/0` | Meridian proxy (CDN traffic) |
| 9090 | 3 node EIPs only | Admin API — has purge/migrate endpoints. NOT public |
| 5432 | 3 node EIPs only | Postgres (Virginia only). NOT public |

Port 9090 is the admin API with cache purge and live migration endpoints. If
this were public, anyone could purge your cache or trigger a policy migration.
Restricting it to the 3 node IPs means only inter-cluster health checks and
your SSH tunnel can reach it.

**EC2 instances** — Ubuntu 22.04 LTS, t3.small, 20GB gp3 SSD. The `user_data`
field runs a bootstrap script on first boot (see below).

**EIP associations** — link Elastic IPs to instances after both exist.

### route53.tf

Creates a Route 53 hosted zone and 3 A records, all for the same hostname
(`proxy.meridian.sricharan.dev`). Each record has:

- A different `set_identifier` (virginia, ireland, tokyo)
- A `latency_routing_policy` pointing to its AWS region
- TTL of 60 seconds

When a DNS resolver queries this hostname, Route 53 returns the IP of
whichever region has the lowest latency from that resolver's network location.

The 60-second TTL means: if a node goes down, clients that cached the old IP
keep trying it for up to 60 seconds before their DNS cache expires and they
re-resolve to a healthy node. Lower TTL = faster failover but more DNS lookups
(negligible cost at this scale).

After `terraform apply`, the output shows NS records that you must add to your
DNS provider (e.g. Vercel) as an NS delegation. This tells the world "for
anything under meridian.sricharan.dev, ask Route 53."

### outputs.tf

Displayed after `terraform apply`. Includes:
- Elastic IPs per region (for SSH, scripts, debugging)
- Ready-to-paste SSH commands
- Route 53 NS records with instructions for Vercel delegation
- Postgres connection string (password omitted — reference `var.db_password`)
- Security warning about secrets in `terraform.tfstate`

### templates/user_data.sh.tpl

EC2 "user data" is a bash script that runs once on first boot, as root. Ours
is a Terraform template — `templatefile()` injects values that only exist at
apply time (DB password, license key, peer IPs).

What the script does:
1. Installs Redis, jq, curl, unzip
2. On Virginia only: installs Postgres, configures `pg_hba.conf` to allow
   connections from the 3 Elastic IPs, creates the `meridian` database and user
3. Configures Redis to bind to 127.0.0.1 only (security — no remote access)
4. Creates a `meridian` system user and `/opt/meridian/{bin,config,data}` dirs
5. If MaxMind license key is provided: downloads GeoLite2-City.mmdb
6. Installs the systemd unit file so `systemctl start meridian` works
7. Writes `/opt/meridian/.provisioned` as a completion marker

All output goes to `/var/log/meridian-provision.log` for debugging.

The script uses `set -euo pipefail` — any command failure aborts the entire
script immediately rather than silently continuing with a half-configured
system.

## Scripts explained

### deploy.sh

Replaces CI/CD entirely. No GitHub Actions, no external services.

1. Reads instance IPs from `terraform output`
2. Cross-compiles: `GOOS=linux GOARCH=amd64 go build`
3. SCPs the binary to `/opt/meridian/bin/` on each instance
4. Restarts the systemd service

Run `./deploy.sh` for all 3 nodes or `./deploy.sh virginia` for one.

Why not CI/CD? This is a side project. GitHub Actions adds another service to
maintain, and you might stop the EC2s to save costs. A script you run from
your laptop is simpler, has no monthly cost, and gives you direct control.

### setup-postgres.sh

Initializes the Postgres database on the Virginia instance:
1. Creates the `meridian` database and user (idempotent — safe to re-run)
2. SCPs and runs `schema/migrations/001_initial.sql`
3. Verifies tables exist

This is separate from the user data script because the schema might change
across versions, and you want to run migrations explicitly rather than
coupling them to instance provisioning.

### generate-config.sh

Generates a `meridian.yaml` config file for a specific node. It reads the
Elastic IPs from Terraform output and fills in the cluster section with the
correct coordinates and addresses.

Why a script instead of a static file? The IPs aren't known until
`terraform apply` finishes. The config needs all 3 IPs for the cluster
health check section.

### status.sh

SSHes into each instance and checks:
- Is the systemd service running?
- What does `/healthz` return?
- When was the instance provisioned?

Quick way to verify everything is alive after a deploy.

### stop-all.sh

Stops the Meridian service on all instances. With `--terminate`, also runs
`terraform destroy` to delete all AWS resources (with a confirmation prompt).

Use this for cost management — stop the services when you're not
demonstrating, destroy everything when you're done with the project.

## systemd/meridian.service

The systemd unit file defines how Linux manages the Meridian process:

- Runs as the `meridian` user (not root)
- Restarts automatically on crash (5-second delay)
- Starts after the network and Redis are ready
- Sets `MERIDIAN_DEPLOYMENT_ID` to the hostname (for event tracking)

This file is installed by the user data script during provisioning AND kept
in the repo so `deploy.sh` can update it if needed.

## What terraform.tfstate contains

Terraform tracks every resource it created in `terraform.tfstate`. This file:
- Maps resource names to real AWS IDs (instance IDs, EIP allocations, etc.)
- Contains the DB password in plaintext (because Terraform needed it to
  configure Postgres)
- Is required to modify or destroy the infrastructure later

**Never commit this file.** It's already in `.gitignore`. For team use,
configure an S3 backend so state is stored remotely with encryption.

## Cost estimate

| Resource | Count | Approximate monthly cost |
|---|---|---|
| t3.small instances | 3 | ~$15/each = $45 |
| Elastic IPs (attached) | 3 | Free |
| Route 53 hosted zone | 1 | $0.50 |
| Route 53 queries | - | ~$0.40/million queries |
| 20GB gp3 EBS volumes | 3 | ~$2.40/each = $7.20 |
| **Total** | | **~$53/month** |

Stop instances when not in use to reduce costs. Elastic IPs incur a small
charge (~$3.60/month each) when NOT attached to a running instance, so either
keep instances running or release the EIPs with `terraform destroy`.
