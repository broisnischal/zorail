# Zorail

**Self-hosted disposable inboxes for organizations.** The private, secure
alternative to Mailinator / temp-mail.com — run your own SMTP sink so that test
mail (signup flows, OTPs, magic links, password resets) never leaves your infra
and never lands in a public inbox anyone can read.

Built for teams that test their own products: point your staging app's email at
`anything@your-zorail-domain`, then read it back programmatically.

> **Status: MVP — SMTP ingest + storage + JSON API + bundled web UI.** Zorail
> accepts inbound mail, parses it (MIME, attachments, charsets, RFC 2047
> headers), persists it to SQLite, and serves it through a JSON API and a
> YOPmail-style web dashboard embedded directly in the binary. Scoped per-key
> auth, server-side spam scoring, AI-powered extraction, and pluggable AI
> providers are designed-for but not yet implemented — see [Roadmap](#roadmap).

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

If `ZORAIL_API_TOKEN` is set, every `/api` call (except health) requires it via
`Authorization: Bearer <token>` or `?token=<token>`. The UI has a ⚙ button to
store the token in the browser.

Example — poll an inbox from a test suite:

```bash
curl -s "http://localhost:8080/api/inboxes/qa-1%40your.domain/messages" \
  -H "Authorization: Bearer $ZORAIL_API_TOKEN"
```

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

Next, in priority order:

1. **Long-poll / webhook** — "wait for the next mail in inbox X" so test suites
   block instead of busy-polling.
2. **Scoped API keys** — per-team / per-project keys with inbox-prefix scoping
   and rate limits, so one Zorail serves a whole org safely.
3. **Pluggable AI providers** — summarize and classify via the `internal/ai`
   provider interface (Claude / Mistral / Ollama), selectable per deployment or
   per key, including a fully local/offline option for air-gapped self-hosting.
4. **Retention/TTL** — auto-expire disposable inboxes after a configurable age.

## Security notes

- Zorail is **receive-only**; it never relays mail.
- Always set `ZORAIL_ALLOWED_DOMAINS` in production so you are not an open sink.
- Run behind a firewall or TLS terminator; STARTTLS support is wired in
  (`smtp.New` accepts a `*tls.Config`) and will be exposed via config next.
- The `internal/ai` seam means AI processing is opt-in and provider-selectable —
  choose a local model when test mail content must never leave the box.

## License

TBD.
