# Zorail

**Self-hosted disposable inboxes for organizations.** The private, secure
alternative to Mailinator / temp-mail.com — run your own SMTP sink so that test
mail (signup flows, OTPs, magic links, password resets) never leaves your infra
and never lands in a public inbox anyone can read.

Built for teams that test their own products: point your staging app's email at
`anything@your-zorail-domain`, then read it back programmatically.

> **Status: multi-tenant — three address modes, scoped auth, MCP, forwarding.**
> Zorail accepts inbound mail (SMTP or HTTP ingest), parses it (MIME,
> attachments, charsets, RFC 2047 headers), persists it to SQLite, and serves it
> through a JSON API, a YOPmail-style web dashboard, and an **MCP server** — all
> embedded in one static binary. It now offers **disposable**, **reserved**, and
> **forwarding** addresses; **user accounts with scoped API keys**; a long-poll /
> MCP `wait_for_message`; and retention sweeping. Pluggable AI providers remain
> designed-for but not yet implemented — see [Roadmap](#roadmap).

## Why

Public temp-mail services are a security liability: your verification codes,
reset links, and PII transit and persist on infrastructure you don't control,
often in inboxes that are world-readable by design. Zorail gives you the same
disposable-inbox convenience while keeping every byte inside your own network.

## Features (today)

- **Catch-all ingest** — every address at your domain(s) is a live inbox, no
  provisioning. `qa-run-7@mail.yourorg.test` just works the moment mail arrives.
- **Domain scoping** — restrict which recipient domains are accepted so you're
  not an open sink.
- **Robust parsing** — multipart MIME, attachments, non-UTF-8 charsets, and
  RFC 2047 encoded headers are decoded; malformed mail is still stored (raw is
  always preserved) rather than dropped.
- **Self-contained binary** — pure-Go SQLite (no cgo) and the **web UI embedded
  via `go:embed`**, so it ships as one static binary and one distroless
  container. No external database, no separate frontend service, no runtime
  Node.
- **Bundled web UI** — a **Nuxt 4 SPA** with a minimal monochrome theme (oklch
  neutral grays + one settable accent, Geist / Geist Mono), statically generated
  and embedded into the binary. Fonts self-hosted at build time → zero runtime
  network deps. Features:
  - **Command palette** (`⌘K`) — jump to any inbox, generate a disposable
    address, or run any action.
  - **Generate disposable address** — one click/key (`g`) mints `qa-1234@your.domain`,
    copies it, and opens the inbox.
  - **Proper HTML rendering** — emails render in a CSP-sandboxed, auto-sized
    iframe with **remote images blocked by default** (tracking-pixel
    protection) and a one-click "load images" toggle. Tabs for rendered / text
    / **headers** / raw / attachments.
  - **Smart detection** — OTP **codes**, **links**, and **unsubscribe** targets
    surfaced as chips (computed server-side), plus a **spam score** with reasons.
  - **Search** across the inbox and all mail, **read/unread** tracking, pinned
    inboxes, **keyboard navigation** (`j/k`, `enter`, `x`, `/`), toasts,
    skeletons, light/dark themes, and accent colors.
- **JSON API** — list inboxes, read/delete messages, fetch raw source and
  attachments; optional bearer-token auth. The automation surface for test
  suites.
- **Per-recipient fan-out** — a message to N recipients becomes N independent
  inbox copies, each independently readable/deletable.

## Architecture

```
                 ┌──────────────┐     ┌───────────────┐     ┌──────────────┐
   inbound  ───▶ │ smtp (sink)  │ ──▶ │  mailparse    │ ──▶ │   storage    │
   SMTP          │ go-smtp      │     │  MIME decode  │     │  SQLite      │
                 └──────────────┘     └───────────────┘     └──────┬───────┘
                        │                                          │
                        └── per-recipient fan-out ─────────────────┤
                                                                   ▼
   browser ◀──────────────────── http api + embedded web ui ◀── internal/api
                                  (JSON + go:embed dashboard)

   internal/ai  ── provider seam (Claude / Mistral / Ollama) — later phase
```

Package layout:

| Package                  | Responsibility                                        |
|--------------------------|-------------------------------------------------------|
| `cmd/zorail`             | Entrypoint: config, wiring, graceful shutdown         |
| `internal/config`        | Env-based configuration                               |
| `internal/smtp`          | Inbound SMTP backend + session (receive-only)         |
| `internal/mailparse`     | Raw bytes → `model.Message` (tolerant MIME parsing)   |
| `internal/api`           | JSON API + embedded web UI (`internal/api/web`)       |
| `ui/`                    | Nuxt 4 SPA source; `make ui` generates → `internal/api/web` |
| `internal/storage`       | `Store` interface + errors                            |
| `internal/storage/sqlite`| SQLite implementation (pure Go)                       |
| `internal/model`         | Core domain types                                     |
| `internal/id`            | Sortable ULID-style IDs (stdlib only)                 |
| `internal/ai`            | Provider-agnostic AI seam (interface only, for now)   |

## Web UI & API

Once running, open the dashboard at **http://localhost:8080**. Type any address
at your domain to open its inbox; messages stream in and auto-refresh.

The JSON API under `/api`:

| Method & path                                   | Description                          |
|-------------------------------------------------|--------------------------------------|
| `GET /api/health`                               | Liveness (always open)               |
| `GET /api/config`                               | Domain, version, auth flag (open)    |
| `GET /api/inboxes`                              | All inboxes with counts              |
| `GET /api/inboxes/{inbox}/messages`             | Messages in an inbox (metadata)      |
| `GET /api/search?q=`                            | Search subject/sender/body, all mail |
| `GET /api/messages/{id}`                        | Full message + `extracted` (codes/links/unsubscribe) + `spam` |
| `GET /api/messages/{id}/raw`                    | Raw RFC 5322 source                  |
| `GET /api/messages/{id}/attachments/{aid}`      | Download an attachment               |
| `DELETE /api/messages/{id}`                     | Delete a message                     |
| `DELETE /api/inboxes/{inbox}`                   | Clear an inbox                       |
| `GET /api/inboxes/{inbox}/wait?timeout=&after=` | **Long-poll**: block until the next message arrives (204 on timeout) |
| `POST /api/ingest`                              | Push mail in over HTTP (Cloudflare Worker / relay → Zorail) |
| `POST /api/auth/register` · `/login`            | Create a user · log in (returns a `zk_` manage key) |
| `GET·POST /api/keys` · `DELETE /api/keys/{id}`  | List / mint / revoke scoped API keys |
| `GET·POST /api/addresses` · `PATCH·DELETE …/{address}` | Reserve / configure / release reserved & forwarding addresses |
| `POST /api/verify/request` · `GET /api/verify/confirm` | Verify a forwarding destination mailbox |

If `ZORAIL_API_TOKEN` is set, every `/api` call (except health/config) requires
it via `Authorization: Bearer <token>` or `?token=<token>`. The UI has a ⚙
button to store the token in the browser.

Example — poll an inbox from a test suite:

```bash
curl -s "http://localhost:8080/api/inboxes/qa-1%40your.domain/messages" \
  -H "Authorization: Bearer $ZORAIL_API_TOKEN"
```

## Three address modes

Zorail serves three behaviors on one ingest engine (see [docs/PRD.md](docs/PRD.md)):

- **Disposable** — catch-all, ephemeral, auto-expiring (set `ZORAIL_RETENTION_DAYS`). The default; no setup.
- **Reserved** — a permanent address claimed by a user (`POST /api/addresses {type:"reserved"}`); never swept.
- **Forwarding** — a reserved address that re-emits each message to a **verified** external mailbox (your Gmail/Outlook). `POST /api/addresses {type:"forward", forward_to:[…]}`, then verify the destination.

### Users & scoped keys

Replace the single global token with real accounts and **scoped API keys**:
register → log in → mint keys with scopes (`read` / `manage` / `admin`) and an
optional **inbox-prefix** so one Zorail safely serves many teams. The legacy
`ZORAIL_API_TOKEN`, when set, is accepted as an implicit admin key.

### Forwarding: how delivery works (and why it's delegated)

Zorail **never runs raw outbound SMTP with its own IP reputation.** Forwarded
mail is re-emitted **verbatim** (preserving the original DKIM signature) with the
envelope sender rewritten to a Zorail-owned bounce address (**SRS-lite**, so SPF
aligns). Two supported deployments:

- **Cloudflare Email Routing (no infra):** Cloudflare is the MX and forwards
  natively; an Email Worker also `POST`s each message to `/api/ingest` so Zorail
  keeps a searchable copy. Leave `ZORAIL_RELAY_*` empty.
- **Relay smarthost:** set `ZORAIL_RELAY_HOST` (Resend/Postmark/SES SMTP or any
  submission server). The built-in forward worker delivers queued mail through
  it with retry/backoff. Destinations must be verified first
  (`/api/verify/request`).

## MCP server (for AI agents)

Zorail exposes a [Model Context Protocol](https://modelcontextprotocol.io) server
(official Go SDK, Streamable HTTP) at **`/mcp`**, authenticated with the same
`zk_` keys. Tools: `create_disposable_address`, `list_inboxes`, `list_messages`,
`read_message`, `delete_message`, and **`wait_for_message`** — which *blocks*
until the next mail arrives and returns the extracted OTP codes/links. That makes
Zorail the email step in agentic end-to-end tests: mint an address → trigger the
signup → `wait_for_message` → read the code.

```jsonc
// MCP endpoint:  POST http://localhost:8080/mcp
// Header:        Authorization: Bearer zk_…
```

## zmail — live terminal client

`zmail` is an interactive TUI that watches your inboxes **live in the terminal** —
no browser. It talks to a running server over the same JSON API (including the
long-poll `/wait` endpoint), so new mail pushes in instantly, and it works
against a local or remote server.

```bash
make cli                       # build bin/zmail
./bin/zmail                    # connect to http://127.0.0.1:8090 (default)
./bin/zmail --url https://mail.example.com --token zk_…   # remote + auth
# or, without building:        make watch URL=… TOKEN=…
```

Environment: `ZORAIL_URL` (default `http://127.0.0.1:8090`) and `ZORAIL_TOKEN`.

A three-pane browser — inboxes · messages · reader — that auto-refreshes and
flashes when mail lands. Detected OTP **codes** and **links** are surfaced at the
top of each message and copyable with one key (clipboard + OSC52, so it works
over SSH too).

```
 zorail zmail  ·  ● live                                    3 inboxes · @localhost
╭ INBOXES  3 ─╮╭ INBOX watch-me@localhost ─╮╭ Verify your account ──────────╮
│ ▎ watch-me  ││ ▎ stripe@billing.test     ││ from  noreply@myapp.test       │
│   now       ││   now                     ││ date  Jun 30 15:49 · now ago   │
│   2 msg     ││   Your receipt            ││ CODES                          │
│   qa-1      ││   noreply@myapp.test      ││  884217                        │
│   19m       ││   16s                     ││ ─────────────────────────────  │
│   1 msg     ││   Verify your account     ││ Your verification code is …    │
╰─────────────╯╰───────────────────────────╯╰────────────────────────────────╯
 ✦ new mail · Your receipt        j/k move  ↵ read  c code  y copy  / search  ? help
```

Keys: `j/k` move · `↵` drill in · `←/esc` back · `tab` switch pane · `g` generate
address · `c` copy code · `y` copy address/sender · `d` delete · `D` clear inbox ·
`/` search (across all inboxes) · `r` refresh · `?` help · `q` quit.

### Receive real mail on localhost — `zmail setup`

`zmail setup` wires a real domain's inbound mail into your localhost server in
one shot, using **Cloudflare Email Routing + an Email Worker + a Cloudflare
Tunnel** — no public IP, no open port 25, free:

```
sender → Cloudflare (MX) → Email Worker → HTTPS POST /api/ingest
                                              ↑
                          Cloudflare Tunnel (cloudflared) → your localhost:8090
```

Run it on the machine hosting the server (the domain must already be on
Cloudflare). With a Cloudflare API token it automatically: creates a Tunnel and
points a hostname at `localhost`, deploys the ingest Worker, enables Email
Routing and adds the MX/SPF records, and sets a catch-all rule `*@domain →
Worker`. It also locks down the server (provisions `ZORAIL_API_TOKEN`) so the
now-public ingest endpoint can't be abused.

```bash
make cli                                   # build bin/zmail
./bin/zmail setup --domain example.com     # or: make setup DOMAIN=example.com
# then run the tunnel it prints (once, persistent):
sudo cloudflared service install <token>
./bin/zmail doctor                         # verify the whole pipeline end-to-end
```

The Cloudflare API token needs **Zone:Email Routing, Zone:DNS, Account:Workers
Scripts, Account:Cloudflare Tunnel** edit permissions. `zmail doctor` re-checks
every link (routing, worker, tunnel health, server auth) and pushes a live probe
through the public ingress to confirm mail reaches your inbox.

## Quick start

### Local (dev)

```bash
make run          # listens on :1025, catch-all for any domain
```

Send a test message from another shell:

```bash
python3 - <<'PY'
import smtplib
from email.mime.text import MIMEText
m = MIMEText("Your code is 552310. Verify: https://app.test/v?t=abc")
m["Subject"]="Confirm signup"; m["From"]="noreply@app.test"; m["To"]="qa-1@localhost"
s = smtplib.SMTP("127.0.0.1", 1025); s.send_message(m); s.quit()
PY
```

Inspect what landed (until the API exists):

```bash
sqlite3 zorail.db "SELECT inbox, subject, text_body FROM messages;"
```

Then open **http://localhost:8080** for the dashboard.

### Run from GHCR (prebuilt image)

A multi-arch image (`linux/amd64`, `linux/arm64`) is published to GitHub
Container Registry by `.github/workflows/docker-publish.yml`:

```bash
docker run -d --name zorail \
  -p 25:25 -p 8080:8080 \
  -e ZORAIL_DOMAIN=mail.yourorg.test \
  -e ZORAIL_ALLOWED_DOMAINS=mail.yourorg.test \
  -v zorail-data:/data \
  ghcr.io/nees/zorail:latest
```

> Replace `nees` with your GitHub org/user — the image path is
> `ghcr.io/<owner>/zorail`. After the first push, make the package public (or
> `docker login ghcr.io`) to pull it. No secrets are needed in CI: the workflow
> authenticates with the built-in `GITHUB_TOKEN` (needs `packages: write`,
> already set in the workflow).

### Docker Compose (self-hosting)

```bash
docker compose up -d            # pulls ghcr.io/<owner>/zorail:latest
# or build locally: uncomment `build: .` in docker-compose.yml, then:
docker compose up --build -d
```

Edit `docker-compose.yml` to set `ZORAIL_DOMAIN` and `ZORAIL_ALLOWED_DOMAINS`,
then point your domain's MX record at the host running Zorail.

For a full cloud walk-through — provisioning an EC2 instance, the exact
Cloudflare DNS records (MX + the grey-cloud gotcha), HTTPS for the dashboard via
Caddy, and a port-25-free path using a Cloudflare Email Worker → `/api/ingest` —
see **[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)**.

## Configuration

All via environment variables:

| Variable                   | Default       | Description                                            |
|----------------------------|---------------|--------------------------------------------------------|
| `ZORAIL_SMTP_ADDR`         | `:1025`       | Listen address. Use `:25` in production.               |
| `ZORAIL_DOMAIN`            | `localhost`   | SMTP greeting / HELO domain.                           |
| `ZORAIL_ALLOWED_DOMAINS`   | *(empty)*     | Comma-separated recipient domains to accept. Empty = accept all (dev only). |
| `ZORAIL_MAX_MESSAGE_BYTES` | `26214400`    | Reject `DATA` larger than this (25 MiB).               |
| `ZORAIL_MAX_RECIPIENTS`    | `100`         | Max `RCPT` per transaction.                            |
| `ZORAIL_HTTP_ADDR`         | `:8080`       | Listen address for the web UI + JSON API.              |
| `ZORAIL_API_TOKEN`         | *(empty)*     | When set, `/api` requires this bearer token. Empty = open. |
| `ZORAIL_DB_PATH`           | `zorail.db`   | SQLite file path.                                      |
| `ZORAIL_RETENTION_DAYS`    | `0`           | Sweep disposable mail older than N days (0 = never). Reserved/forward addresses are exempt. |
| `ZORAIL_RELAY_HOST`        | *(empty)*     | Outbound relay/smarthost for forwarding. Empty = forwarding delegated (e.g. to Cloudflare Email Routing). |
| `ZORAIL_RELAY_PORT`        | `587`         | Relay port (587 STARTTLS submission).                  |
| `ZORAIL_RELAY_USER`        | *(empty)*     | Relay username (empty = no auth).                      |
| `ZORAIL_RELAY_PASS`        | *(empty)*     | Relay password.                                        |
| `ZORAIL_RELAY_FROM`        | `bounces@<domain>` | Envelope `MAIL FROM` for forwards (SRS-lite, SPF alignment). |
| `ZORAIL_FORWARD_MAX_TRIES` | `5`           | Give up forwarding after this many attempts.           |
| `ZORAIL_LOG_LEVEL`         | `info`        | `debug` \| `info` \| `warn` \| `error`.               |

## Development

**Backend (Go):**

```bash
make test    # unit tests + full SMTP→parse→SQLite + API integration tests
make build   # static binary at bin/zorail (embeds the current UI bundle)
make tidy    # go mod tidy
```

**Frontend (Nuxt UI in `ui/`)** — requires Node 22+ and `pnpm`:

```bash
make ui      # build the SPA and copy it into internal/api/web (the embed dir)
make dev     # Nuxt hot-reload dev server on :3000, proxying /api -> :8090
```

The generated bundle in `internal/api/web/` is committed so `go build`/`go test`
work without Node. Run `make ui` after changing anything under `ui/` to refresh
it. The Docker image rebuilds the UI from source regardless (Node stage in the
`Dockerfile`), so the published image always matches `ui/`.

Typical UI dev loop: terminal 1 `make run` (Go API on :8090), terminal 2
`make dev` (Nuxt on :3000 with live reload, API proxied). When done, `make ui`
to bake the result into the binary.

## Roadmap

Done:

- ✅ **SMTP ingest + storage** — tolerant MIME parsing, per-recipient fan-out.
- ✅ **JSON API** — list inboxes, read/delete messages, raw + attachments.
- ✅ **Web dashboard** — Nuxt SPA: command palette, HTML rendering with image
  blocking, search, read/unread, themes, keyboard nav.
- ✅ **Server-side smart extraction** — OTP codes, links, and `List-Unsubscribe`
  targets as first-class API fields (`internal/extract`).
- ✅ **Spam scoring** — explainable heuristic score + reasons per message.
- ✅ **Search** across all mail.
- ✅ **Optional token auth** on the API/UI.
- ✅ **GHCR publishing** — multi-arch image via GitHub Actions.
- ✅ **Long-poll** — `GET …/wait` and the MCP `wait_for_message` tool block until
  the next message arrives instead of busy-polling.
- ✅ **User accounts + scoped API keys** — `read`/`manage`/`admin` scopes with
  optional inbox-prefix scoping, so one Zorail serves a whole org safely.
- ✅ **Reserved & forwarding addresses** — permanent claimed inboxes, and
  forwarding to verified external mailboxes via a relay (or delegated to
  Cloudflare Email Routing).
- ✅ **HTTP ingest** (`POST /api/ingest`) — receive mail without opening port 25.
- ✅ **MCP server** — agent-native tools over Streamable HTTP.
- ✅ **Retention/TTL** — `ZORAIL_RETENTION_DAYS` sweeps disposable mail (reserved
  & forwarding addresses exempt).

Next, in priority order:

1. **Pluggable AI providers** — summarize and classify via the `internal/ai`
   provider interface (Claude / Mistral / Ollama), selectable per deployment or
   per key, including a fully local/offline option for air-gapped self-hosting.
2. **Per-key rate limits** — the key table is ready; enforcement is deferred.
3. **Reverse-alias replies** — reply from a forwarding alias (SimpleLogin-style),
   and ARC sealing so a "forwarded by Zorail" banner won't break DMARC.

## Security notes

- Disposable/reserved modes are **receive-only**. **Forwarding sends** — it is
  delegated to a relay/Cloudflare and only delivers to **verified** destinations,
  so Zorail is not an open relay.
- By default `ZORAIL_ALLOWED_DOMAINS` is unset → Zorail accepts mail for **every**
  recipient domain whose MX points at it (open catch-all). Set it to a
  comma-separated list only if you want to restrict which domains are accepted.
- Set `ZORAIL_API_TOKEN` (admin) and/or use scoped `zk_` keys; keys are stored as
  sha-256 hashes, passwords as bcrypt.
- Run behind a firewall or TLS terminator; STARTTLS support is wired in
  (`smtp.New` accepts a `*tls.Config`) and will be exposed via config next.
- The `internal/ai` seam means AI processing is opt-in and provider-selectable —
  choose a local model when test mail content must never leave the box.

## License

TBD.
