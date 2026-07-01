# Connecting a real domain to Zorail

This guide wires a domain's inbound mail to a Zorail server running on your
machine. Anything sent to `*@your-domain` lands in your local inbox — no public
IP, no port 25, no separate frontend.

## How it works

```
sender → Cloudflare Email Routing → Email Worker → Cloudflare Tunnel → http://localhost/api/ingest → Zorail
```

`zorail setup` provisions the Cloudflare side (tunnel, DNS, worker, routing,
catch-all) via the Cloudflare API. `zorail up` runs the two local processes (the
Zorail server and `cloudflared`). You run `setup` once per domain; after that you
just `zorail up`.

## Prerequisites

- A domain on **Cloudflare** (its nameservers point at Cloudflare).
- [`cloudflared`](https://pkg.cloudflare.com/) installed — `brew install cloudflared`
  (macOS), your distro's package, or the binary from Cloudflare.
- The `zorail` binary (see the README's Install section) — or Go, to run from source.

## The commands, in order

```bash
# 1. Start the server so setup can reach it (any terminal, leave it running).
zorail                       # runs on :8080 by default

# 2. Provision Cloudflare: pick your domain, paste an API token once.
zorail setup                 # auto-detects the server's port; checks every
                             # permission up front before touching anything

# 3. Run the server + Cloudflare Tunnel together — the normal way to run Zorail.
zorail up

# 4. Verify the whole pipeline (another terminal).
zorail doctor                # expect all ✓

# 5. Watch mail arrive live.
zorail watch                 # auto-reads the URL + token from .env
```

Send a message to `anything@your-domain` and it appears in `zorail watch`.

> Running from a source checkout? The `make` targets wrap these: `make run`,
> `make setup`, `make up`, `make doctor`, `make watch`.

## The Cloudflare API token

`setup` asks for a token **once** and saves it to `.env`, so you never paste it
again. Create it at **<https://dash.cloudflare.com/profile/api-tokens> → Create
Custom Token** (a **user-owned** token — *not* an Account-owned one; the Tunnel
API rejects account-owned tokens).

`setup` opens a pre-filled link, but **Cloudflare only pre-fills a few
permissions** (DNS, Zone, Workers Scripts). You must add the rest with the
**“+ Add more”** button. The complete set:

| Scope | Permission | Level | Pre-filled? |
| --- | --- | --- | --- |
| Zone | Zone | Read | ✅ auto |
| Zone | DNS | Edit | ✅ auto |
| Account | Workers Scripts | Edit | ✅ auto |
| Account | **Cloudflare Tunnel** | Edit | ➕ add manually |
| Zone | **Email Routing Rules** | Edit | ➕ add manually |
| Zone | **Zone Settings** | Edit | ➕ add manually |
| Account | Email Routing Addresses | Read | ➕ only if you use forwarding |

- **Account Resources:** Include → **All accounts** (or the account that owns
  your domain — a wrong/narrow scope causes `10000` on account calls).
- **Zone Resources:** Include → All zones (or your specific zone).

Why each is needed: *Cloudflare Tunnel* creates the tunnel; *Workers Scripts*
deploys the ingest worker; *DNS* writes the CNAME + MX/SPF records; *Zone Read*
finds your zone; *Email Routing Rules* sets the catch-all; **Zone Settings**
enables Email Routing (this is the one everyone misses — the Email Routing
enable/status endpoints are gated by Zone Settings, not by the Email Routing
permissions).

`setup` runs a **permission preflight** right after you pick the domain: it
prints a ✓/✗ line per permission and stops *before* provisioning if any are
missing, telling you exactly what to add. So if you forget one, you fix the
token once and re-run — no half-finished state.

## What setup writes to `.env`

Setup writes a complete config to the **repo-root** `.env` (the file the server
auto-loads):

```
CLOUDFLARE_API_TOKEN=...     # reused by setup/doctor/up so you aren't re-prompted
ZORAIL_API_TOKEN=zt_...      # the worker authenticates ingest with this; the server requires it
ZORAIL_DOMAIN=your-domain
ZORAIL_ALLOWED_DOMAINS=your-domain
ZORAIL_HTTP_ADDR=:8080       # whatever port your server was found on
ZORAIL_SMTP_ADDR=:1025
ZORAIL_TUNNEL_TOKEN=...       # lets `zorail up` start cloudflared
```

`setup`, `doctor`, and `watch` all read this file, so they target the right
port and token automatically. Keep `.env` private (it's `chmod 600`) and out of
git.

## Running as a background service (daemon)

To have Zorail start on boot and restart on failure, generate a systemd unit:

```bash
zorail service --mode up | sudo tee /etc/systemd/system/zorail.service >/dev/null
sudo systemctl daemon-reload
sudo systemctl enable --now zorail
journalctl -u zorail -f            # logs
```

- `--mode up` = server + Cloudflare Tunnel (needs `cloudflared` + a completed
  `setup`). `--mode serve` = local server only.
- `--user` generates a no-sudo per-user service (`~/.config/systemd/user`; run
  `loginctl enable-linger "$USER"` so it survives logout). Note a user service
  can't bind privileged ports like SMTP `:25`.

## Starting over — `zorail reset`

```bash
zorail reset          # asks to confirm, lists what it removes
zorail reset --yes    # skip the prompt
```

Removes the local **database** (`zorail.db` + `-wal`/`-shm`), the **setup state**
file, and your **`.env`** (backed up to `.env.bak`). It does **not** touch
Cloudflare — remove the tunnel/Worker/DNS/routing from the dashboard if you want
a full teardown. Stop the running server first, or it keeps using the deleted DB.

## Troubleshooting

**`cloudflare error 10000` during `setup`**
A missing token permission or a wrong Account Resources scope. The preflight now
names the exact one; add it (“+ Add more”), recreate the token, re-run `setup`.
The most common miss is **Zone Settings: Edit** (Email Routing enable) and
**Cloudflare Tunnel: Edit**.

**`couldn't enable Email Routing via API: … 10000`**
Add **Zone → Zone Settings → Edit** to the token. (Or just enable it once in the
dashboard: your-domain → Email → Email Routing → Enable.)

**`HTTP 530` / `error code: 1033` on the end-to-end probe**
The tunnel connector isn't running. `1033` = "tunnel has no origin." Start it
with `zorail up` and leave it running (it's a daemon), or install the
`zorail service` unit.

**`doctor`: "Local server reachable — server is in OPEN mode"**
The running server has no `ZORAIL_API_TOKEN`. Stop it and start via `zorail up`
from the repo root so it loads `.env`:
`lsof -ti tcp:8080 | xargs -r kill && zorail up`.

**`watch`/`setup`: connection refused, or wrong port**
Tooling now auto-detects the port from `.env` (`ZORAIL_HTTP_ADDR`) and falls back
to `:8090`/`:8080`. If it still can't connect, the server isn't running — start
it (`zorail` or `zorail up`) first.

**`zorail up`: "no ZORAIL_TUNNEL_TOKEN … run `zorail setup` first"**
There's no saved setup (fresh install or after `reset`). Run `zorail setup`
first (it's idempotent — existing tunnel/DNS/worker are reused).

**Address already in use (`:8080`)**
A previous server (or a Docker container) is still bound. Free it:
`lsof -ti tcp:8080 | xargs -r kill` (or `docker rm -f <id>`).

## Rotating credentials

If a token is exposed, roll it: **API Tokens → Roll** in the Cloudflare dashboard
for `CLOUDFLARE_API_TOKEN`, and re-run `zorail setup` to regenerate
`ZORAIL_API_TOKEN` and the tunnel token.
