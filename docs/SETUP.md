# Connecting a real domain to Zorail

This guide wires a domain's inbound mail to a Zorail server running on your
machine. Anything sent to `*@your-domain` lands in your local inbox.

## How it works

```
sender → Cloudflare Email Routing → Email Worker → Cloudflare Tunnel → http://localhost:8090/api/ingest → Zorail
```

`zorail setup` provisions the Cloudflare side (tunnel, DNS, worker, routing,
catch-all) via the Cloudflare API. `zorail up` runs the two local processes (the
Zorail server and `cloudflared`). You only run `setup` once per domain.

## Prerequisites

- A domain on **Cloudflare** (the domain's nameservers point at Cloudflare).
- [`cloudflared`](https://pkg.cloudflare.com/) installed (`brew install cloudflared` on macOS).
- Go (to run from source) — or the prebuilt `zorail` binary.

## The three commands

```bash
# 1. Start a local server once so setup can talk to it (any terminal).
make run

# 2. Provision Cloudflare. Pick your domain from the list, paste a token once.
make setup            # or: ./bin/zorail setup

# 3. Run the server + tunnel together. This is how you normally run Zorail.
make up               # or: ./bin/zorail up

# verify end-to-end (in another terminal)
make doctor           # or: ./bin/zorail doctor
```

That's it. Send a message to `anything@your-domain` and watch it arrive.

## The Cloudflare API token

`setup` asks for a token **once** and saves it to `.env`, so you never paste it
again. Create a **Custom Token** at
<https://dash.cloudflare.com/profile/api-tokens> with exactly these permissions:

| Scope | Permission |
| --- | --- |
| Account | Cloudflare Tunnel : Edit |
| Account | Workers Scripts : Edit |
| Account | Email Routing Addresses : Read |
| Zone | DNS : Edit |
| Zone | Email Routing Rules : Edit |
| Zone | Zone : Read |

**Zone Resources:** Include → All zones (or your specific zone).

All six are required. Missing any one fails partway through setup with a
Cloudflare `10000` "Authentication error" on the first call that needs it.

## What setup writes to `.env`

Setup writes a complete config to the **repo-root** `.env` (the file the server
auto-loads). After a successful run it contains:

```
CLOUDFLARE_API_TOKEN=...     # reused by setup/doctor/up so you aren't re-prompted
ZORAIL_API_TOKEN=zt_...      # the worker authenticates ingest with this; the server requires it
ZORAIL_DOMAIN=your-domain
ZORAIL_ALLOWED_DOMAINS=your-domain
ZORAIL_HTTP_ADDR=:8090
ZORAIL_SMTP_ADDR=:1025
ZORAIL_TUNNEL_TOKEN=...       # lets `zorail up` start cloudflared
```

Keep `.env` private (it's `chmod 600`) and out of git.

## Troubleshooting

**`cloudflare error 10000: Authentication error`**
Your token is missing a permission (or the wrong token is in `.env`). Check it
against the table above. Note: two endpoints — `GET /email/routing` and
`GET /email/routing/dns` — return `10000` for *any* scoped token regardless of
permissions; this is a known Cloudflare quirk and Zorail works around it
automatically (it writes the standard MX/SPF records itself), so you can ignore
those two specifically.

**`HTTP 530` / `error code: 1033` on the end-to-end probe**
The tunnel isn't running. `1033` means "tunnel has no origin." Start it with
`zorail up` (or `cloudflared tunnel run --token <ZORAIL_TUNNEL_TOKEN>`) and leave
that process running — it's a daemon.

**`doctor`: "Local server reachable — server is in OPEN mode"**
The server is running without `ZORAIL_API_TOKEN`. Use `make up` / `make run`
from the repo root so it loads `.env`. (Don't set `ZORAIL_DOMAIN` inline on the
command line — that overrides `.env`.)

**The token went into the wrong `.env`**
Older versions wrote `.env` relative to the current directory, so running from
`bin/` created `bin/.env`. Setup now always targets the repo-root `.env`. If you
have a stray `bin/.env`, delete it.

**`zorail up`: "no ZORAIL_TUNNEL_TOKEN … run `zorail setup` first"**
Your `.env` predates this token being saved. Re-run `zorail setup` (it's
idempotent — existing tunnel/DNS/worker are reused), or pass `--cf-token` so
`up` can fetch the tunnel token from the API.

## Rotating credentials

If a token is ever exposed, roll it: **API Tokens → Roll** in the Cloudflare
dashboard for `CLOUDFLARE_API_TOKEN`, and re-run `zorail setup` to regenerate
`ZORAIL_API_TOKEN` and the tunnel token.
