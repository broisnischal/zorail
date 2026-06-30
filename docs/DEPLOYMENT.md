# Deploying Zorail (AWS EC2 + Cloudflare DNS)

This guide takes you from nothing to a running, internet-reachable Zorail on a
single EC2 instance, with the dashboard served over HTTPS and a domain wired up
to receive mail through Cloudflare.

Substitute these placeholders throughout:

| Placeholder          | Example              | Meaning                                            |
|----------------------|----------------------|----------------------------------------------------|
| `example.com`        | your domain          | The zone you manage in Cloudflare                  |
| `mail.example.com`   | catch-all host       | Inboxes become `anything@mail.example.com`         |
| `inbox.example.com`  | dashboard host       | The web UI + JSON API (HTTPS)                       |
| `203.0.113.10`       | EC2 **Elastic IP**   | Static public IP of the instance                   |

> Want addresses on the apex (`user@example.com`) instead of a subdomain? See
> [Apex vs. subdomain](#apex-vs-subdomain) at the end.

---

## What Zorail exposes

The container (`ghcr.io/<owner>/zorail`) binds two ports:

| Port   | Protocol      | Purpose                                  | Public?                         |
|--------|---------------|------------------------------------------|---------------------------------|
| `25`   | SMTP          | Inbound mail (catch-all)                 | Yes — **for [Path A](#path-a-direct-smtp-port-25) only** |
| `8080` | HTTP          | Web dashboard + JSON API + MCP           | Behind a TLS reverse proxy      |

Zorail is **receive-only** for disposable/reserved inboxes; the only thing that
*sends* is the optional forwarding relay. See the
[Configuration table in the README](../README.md#configuration) for every env var.

---

## Choose how mail reaches Zorail

There are two supported paths. Pick one — they use **mutually exclusive**
Cloudflare DNS setups.

| | [**Path A — Direct SMTP**](#path-a-direct-smtp-port-25) | [**Path B — Cloudflare Email Worker**](#path-b-cloudflare-email-routing--worker-no-port-25) |
|---|---|---|
| How | The world delivers to your `:25` | Cloudflare receives, a Worker `POST`s to `/api/ingest` |
| Port 25 | **Open inbound** on EC2 | **Not used at all** |
| Cloudflare Email Routing | **Off** (you own the MX) | **On** (Cloudflare owns the MX) |
| Best when | You want a classic self-hosted MX | **Recommended on AWS** — avoids port-25 friction entirely |

Both paths share the same **[EC2 setup](#1-provision-the-ec2-instance)** and
**[HTTPS dashboard](#2-run-zorail--caddy-https)**, so do those first.

---

## 1. Provision the EC2 instance

- **Instance:** `t3.micro` or `t3.small`. **Amazon Linux 2023** or Ubuntu 24.04.
- **Allocate an Elastic IP** and associate it, so your DNS A record stays valid
  across stop/start.
- **Security group — inbound rules:**

  | Port | Source         | Why                                            |
  |------|----------------|------------------------------------------------|
  | 22   | **your IP**    | SSH                                            |
  | 80   | `0.0.0.0/0`    | Caddy: ACME challenge + HTTP→HTTPS redirect     |
  | 443  | `0.0.0.0/0`    | Dashboard (HTTPS)                               |
  | 25   | `0.0.0.0/0`    | **Path A only** — inbound SMTP                  |

  > Never expose `8080` publicly — only the local reverse proxy reaches it.

- **Install Docker + Compose:**

  ```bash
  # Amazon Linux 2023
  sudo dnf install -y docker
  sudo systemctl enable --now docker
  sudo usermod -aG docker "$USER"      # then log out/in
  mkdir -p ~/.docker/cli-plugins
  curl -SL https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64 \
    -o ~/.docker/cli-plugins/docker-compose && chmod +x ~/.docker/cli-plugins/docker-compose
  ```

### The two AWS gotchas

1. **Cloudflare cannot proxy SMTP.** Its orange-cloud proxy only handles
   HTTP/HTTPS, so any mail host A record must be **grey-cloud (DNS only)**.
2. **AWS blocks *outbound* port 25 by default.** This affects only the optional
   forwarding feature, and Zorail forwards via port **587**, so you are fine.
   *Inbound* 25 (receiving) works as soon as the security group allows it.

---

## 2. Run Zorail + Caddy (HTTPS)

Zorail serves the dashboard as plain HTTP on `:8080`, so front it with
[Caddy](https://caddyserver.com) for automatic Let's Encrypt TLS. Create a
working directory on the instance with these two files.

**`docker-compose.yml`**

```yaml
services:
  zorail:
    image: ghcr.io/<owner>/zorail:latest    # <-- your GHCR owner
    restart: unless-stopped
    ports:
      - "25:25"                  # inbound SMTP — REMOVE this line if using Path B
      - "127.0.0.1:8080:8080"    # UI/API, localhost only (Caddy reaches it)
    environment:
      ZORAIL_DOMAIN: "mail.example.com"
      ZORAIL_ALLOWED_DOMAINS: "mail.example.com"      # don't be an open sink
      ZORAIL_API_TOKEN: "REPLACE-WITH-LONG-RANDOM"    # protects the API/UI
      ZORAIL_RETENTION_DAYS: "7"                      # sweep old disposable mail
      ZORAIL_DB_PATH: "/data/zorail.db"
      ZORAIL_LOG_LEVEL: "info"
    volumes:
      - zorail-data:/data

  caddy:
    image: caddy:2
    restart: unless-stopped
    network_mode: host           # binds 80/443 and reaches zorail on 127.0.0.1:8080
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config

volumes:
  zorail-data:
  caddy-data:
  caddy-config:
```

**`Caddyfile`**

```
inbox.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Add the dashboard DNS record (needed for both paths so Caddy can get a cert):

| Type | Name    | Content        | Proxy                | Purpose                  |
|------|---------|----------------|----------------------|--------------------------|
| A    | `inbox` | `203.0.113.10` | **DNS only** (grey)  | Dashboard host for Caddy |

Then bring it up:

```bash
docker compose up -d
docker compose logs -f          # watch Caddy fetch the TLS cert
```

Open **https://inbox.example.com**, press `,` for Settings, and paste the same
`ZORAIL_API_TOKEN`.

Now do **one** of the receiving paths below.

---

## Path A — Direct SMTP (port 25)

Keep the `"25:25"` port mapping in the compose file, and **leave Cloudflare Email
Routing disabled**. Add these DNS records:

| Type   | Name   | Content / Target              | Proxy               | Purpose                                  |
|--------|--------|-------------------------------|---------------------|------------------------------------------|
| **A**  | `mail` | `203.0.113.10`                | **DNS only** (grey) | Resolves the mail host to your EC2 IP    |
| **MX** | `mail` | `mail.example.com` · prio `10`| n/a                 | Delivers `*@mail.example.com` to your box |

Rules that trip people up:

- The **MX target must be a hostname** (`mail.example.com`), never an IP, and it
  must point at the **grey-cloud** A record above. Cloudflare rejects an MX whose
  target is proxied.
- **Do not enable Cloudflare Email Routing** on this zone — it installs its own MX
  records and would intercept the mail before it reaches EC2.
- The SMTP listener is currently plain (STARTTLS is wired in the code but not yet
  exposed via config), so inbound mail isn't TLS-encrypted to your box. Receiving
  still works — senders fall back from opportunistic TLS.

Skip to [Verify](#verify-it-works).

---

## Path B — Cloudflare Email Routing + Worker (no port 25)

Here Cloudflare receives the mail and a tiny Email Worker pushes it into Zorail
over HTTPS via `POST /api/ingest`. **Remove the `"25:25"` line** from the compose
file — the instance only needs 80/443.

### B.1 — Mint a manage-scoped key

`/api/ingest` requires the `manage` scope. Create a key (`zk_…`) from the
dashboard (or API) and keep it secret — the Worker authenticates with it.

### B.2 — Enable Email Routing

In Cloudflare → **Email → Email Routing**, enable it for `example.com`. Cloudflare
**adds its own MX + SPF records automatically** — leave them. Then add a **catch-all**
rule → action **Send to a Worker** (you'll bind the Worker next).

### B.3 — Deploy the Worker

```js
// worker.js — Cloudflare Email Worker that forwards raw mail into Zorail.
export default {
  async email(message, env) {
    const raw = await new Response(message.raw).text();
    const url = new URL(env.ZORAIL_INGEST_URL);              // https://inbox.example.com/api/ingest
    url.searchParams.set("rcpt", message.to);                // original recipient
    url.searchParams.set("env_from", message.from);          // envelope sender
    const res = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "message/rfc822",
        "Authorization": `Bearer ${env.ZORAIL_KEY}`,
      },
      body: raw,
    });
    if (!res.ok) message.setReject(`zorail ingest failed: ${res.status}`);
  },
};
```

```toml
# wrangler.toml
name = "zorail-ingest"
main = "worker.js"
compatibility_date = "2024-11-01"

[vars]
ZORAIL_INGEST_URL = "https://inbox.example.com/api/ingest"
# Set the secret with: npx wrangler secret put ZORAIL_KEY
```

```bash
npx wrangler secret put ZORAIL_KEY    # paste the zk_… manage key
npx wrangler deploy
```

Finally, bind this Worker as the catch-all action from step B.2.

> `/api/ingest` also accepts JSON: `POST application/json` with
> `{ "raw": "<RFC822>", "env_from": "...", "rcpts": ["a@…","b@…"] }`. The raw mode
> above (`message/rfc822` + `?rcpt=` + `?env_from=`) is simplest from a Worker.

---

## Verify it works

```bash
# DNS (Path A): does the MX resolve to your box?
dig +short MX mail.example.com
dig +short A  mail.example.com          # -> 203.0.113.10

# Path A: is port 25 open and greeting?
nc -vz mail.example.com 25
swaks --to test123@mail.example.com --server mail.example.com   # send a probe

# Dashboard health (both paths):
curl -fsS https://inbox.example.com/api/health
```

Send a real email to `test123@mail.example.com` and watch it appear in the
dashboard under that inbox.

---

## Security checklist

- [ ] **`ZORAIL_API_TOKEN` set** — without it, anyone reaching the dashboard reads
      every inbox.
- [ ] **`ZORAIL_ALLOWED_DOMAINS` set** — otherwise Zorail accepts mail for any
      domain (open sink).
- [ ] **`8080` not published publicly** — only Caddy/localhost reaches it.
- [ ] **SSH (22) restricted** to your IP.
- [ ] *(Optional, stronger)* flip the `inbox` record to **proxied** and put
      **Cloudflare Access (Zero Trust)** in front of the dashboard. Requires SSL
      mode **Full (strict)** so Caddy's origin cert is trusted.
- [ ] Keep a backup of the `/data` volume (it holds the SQLite DB).

For pure receive-only temp mail you do **not** need SPF/DKIM/DMARC or reverse DNS
— those are sender-side concerns. Add SPF only if you enable the forwarding relay.

---

## Apex vs. subdomain

To use addresses on the apex (`user@example.com`):

- **Path A:** set the **MX `Name` to `@`** (target still `mail.example.com`), keep
  the `mail` A record as the delivery host, and set
  `ZORAIL_DOMAIN`/`ZORAIL_ALLOWED_DOMAINS` to `example.com`.
- **Path B:** enable Email Routing on the apex as usual; the catch-all already
  covers `*@example.com`. Set `ZORAIL_DOMAIN`/`ZORAIL_ALLOWED_DOMAINS` to
  `example.com`.

The subdomain approach (`mail.example.com`) is recommended so it never collides
with any existing email on your apex.
