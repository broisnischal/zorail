# Zorail PRD — Multi-tenant mail: disposable, reserved, and forwarding

**Status:** accepted, in implementation.
**Author:** Zorail team.
**Last updated:** 2026-06-30.

Zorail today is a single-tenant, receive-only disposable-inbox sink: SMTP in →
MIME parse → SQLite → JSON API + web UI, guarded by one optional global token.
This PRD grows it into a **multi-tenant** service offering **three address
modes** on the same ingest engine, secured by **per-user scoped API keys**, and
usable by **AI agents over MCP**.

## Goals

1. **Three address modes** sharing one parse/store pipeline:
   - **Disposable** — catch-all, ephemeral, auto-expiring. *(exists today)*
   - **Reserved** — a permanent address claimed and owned by a user; never
     garbage-collected.
   - **Forwarding** — a reserved address that, on arrival, re-emits the message
     to a verified external destination (the user's Gmail / Outlook / etc.).
2. **Identity + security** — real users and **scoped API keys** (read vs.
   manage vs. admin; optional inbox-prefix / domain scoping), replacing the
   single global token. The global token is retained as an implicit admin key
   for backward compatibility.
3. **MCP server** — expose Zorail as a Model Context Protocol server (official
   Go SDK, Streamable HTTP) so coding/testing agents can mint addresses and,
   critically, **block until a message arrives** (`wait_for_message`) and read
   the OTP/link out of it.
4. **Retention/TTL** — disposable inboxes expire after a configurable age;
   reserved/forwarding addresses are exempt.

## Non-goals

- **Running our own outbound mail server.** Forwarding delivers through a
  configured **relay** (transactional provider or smarthost) or is delegated to
  **Cloudflare Email Routing**. Zorail never operates raw outbound SMTP with its
  own IP reputation. (See "Forwarding" below.)
- **Full IMAP mailboxes.** Reserved addresses are read through Zorail's API /
  UI / MCP, not via IMAP clients. (Mailu / docker-mailserver / Mail-in-a-Box
  remain the tools for that; adopting them would contradict Zorail's
  single-static-binary identity.)
- **Replying from an alias** (SimpleLogin/addy.io reverse-alias). Out of scope
  for v1; revisit once forwarding is stable.

## Competitive grounding

| Source | What we borrow |
|--------|----------------|
| **Mailinator** | API-first disposable model; public vs. private domains; per-inbox isolation. |
| **addy.io (AnonAddy)** — self-hostable | Alias lifecycle: per-address enable/disable, `forward_to`, recipient limits, DMARC-fail warning banner. Closest model to our forwarding mode. |
| **SimpleLogin** | **Destination-mailbox verification** before forwarding (don't become an open relay / backscatter source). |
| **Zoho / Migadu** | The "reserved permanent address" UX; our SQLite + UI is a lightweight mailbox. |
| **Cloudflare Email Routing / ForwardEmail** | The forwarding *engine* we lean on instead of building outbound. |

## The architectural fault line: receive vs. send

Disposable and reserved modes stay **receive-only** — clean extensions of the
existing pipeline. Forwarding requires **sending**, which pulls in the full
deliverability problem:

- **SPF** breaks on a naïve forward (our IP isn't in the original sender's SPF).
  Mitigation: **SRS-lite** — rewrite the envelope `MAIL FROM` to a Zorail-owned
  bounce address so *our* SPF covers the hop.
- **DKIM** survives **only if the body is not modified**. Therefore the
  forwarder is a **verbatim remailer**: it re-emits the original raw bytes
  unchanged. Any "forwarded by Zorail" banner would invalidate DKIM and is
  intentionally omitted in v1 (revisit with ARC later).
- **IP reputation / PTR / DMARC** for the sending domain are owned by the
  **relay**, not by Zorail.

**Decision:** forwarding is delegated. Two supported deployments:

- **(A) Cloudflare Email Routing** (recommended for "no infra"): Cloudflare is
  the MX and performs the forward natively. Zorail still receives a copy via an
  Email Worker → `POST /api/ingest` (see Ingest API). Forwarding config in
  Zorail is then informational/mirrored.
- **(B) Relay smarthost**: Zorail enqueues a forward job and a worker delivers
  it through a configured authenticated relay (Resend / Postmark / SES SMTP, or
  any smarthost). This is what `internal/forward` implements.

## Data model (additions)

```
users(id, email UNIQUE, password_hash, created_at)

api_keys(
  id, user_id -> users.id, name,
  key_hash UNIQUE,                  -- sha-256 of the secret; secret shown once
  scopes,                           -- CSV: read|manage|admin
  inbox_prefix,                     -- optional scope narrowing ("" = any)
  created_at, last_used_at)

addresses(
  address PK,                       -- normalized full address
  type,                             -- disposable|reserved|forward
  owner_user_id -> users.id NULL,   -- NULL for anonymous disposable
  expires_at NULL,                  -- NULL = permanent
  forward_to,                       -- CSV of destinations (forward type)
  forward_enabled,                  -- 0/1 toggle (addy.io-style)
  created_at)

forward_jobs(
  id, message_id, src_address, dest, raw BLOB,
  attempts, next_attempt_at, status,  -- pending|sent|failed
  last_error, created_at)

mailbox_verifications(
  dest PK, user_id, token, verified_at NULL, created_at)
```

`messages` is unchanged except it is now joined to `addresses` by `inbox` for
ownership/scope checks at read time.

## Auth & scopes

- **Key format:** `zk_<base32(16 random bytes)>`. Only the **sha-256 hash** is
  stored; the plaintext is returned exactly once at creation.
- **Scopes:** `read` (list/read/search messages within scope), `manage`
  (reserve/release addresses, configure forwarding, create read-keys),
  `admin` (all users/addresses; the legacy global token maps here).
- **Inbox-prefix scope:** a key may be limited to addresses starting with a
  prefix (e.g. `qa-`), so one Zorail safely serves many teams.
- **Precedence:** if `ZORAIL_API_TOKEN` is set it is accepted as an admin key
  (backward compatible). Otherwise requests authenticate with `zk_…` keys.

## MCP server

- **Transport:** Streamable HTTP at `/mcp`, same binary/port as the API.
- **Auth:** `Authorization: Bearer zk_…`; the key's scope bounds every tool.
- **Tools:**
  - `create_disposable_address(prefix?)` → mints `<prefix|rand>@<domain>`.
  - `list_inboxes()` → inboxes in scope with counts.
  - `list_messages(inbox, limit?)` → metadata.
  - `read_message(id)` → full message + extracted codes/links + spam.
  - `wait_for_message(inbox, timeout_seconds)` → **blocks** until the next
    message arrives (or timeout). The keystone for agentic signup/OTP flows.
  - `delete_message(id)`.
- **Notify hub:** an in-process pub/sub the ingest path signals on every save;
  `wait_for_message` subscribes per inbox. Falls back to DB poll on miss.

## API additions (summary)

```
POST   /api/auth/register            {email,password} -> user
POST   /api/auth/login               {email,password} -> { token: zk_… (manage) }
GET    /api/keys                     list caller's keys (no secrets)
POST   /api/keys                     {name,scopes,inbox_prefix} -> { secret once }
DELETE /api/keys/{id}                revoke

GET    /api/addresses                caller's reserved/forward addresses
POST   /api/addresses                {address|prefix, type, forward_to?} -> reserve
PATCH  /api/addresses/{address}      {forward_to?, forward_enabled?}
DELETE /api/addresses/{address}      release

POST   /api/verify/request           {dest} -> sends verification mail (relay)
GET    /api/verify/confirm?token=…   marks dest verified

POST   /api/ingest                   {raw, env_from?, rcpts[]}  (Worker/relay → Zorail)
GET    /api/inboxes/{inbox}/wait     long-poll: block for next message (HTTP twin of MCP tool)
```

All except `health`, `config`, `register`, `login`, and `verify/confirm`
require a key; scope is enforced per route.

## Rollout sequence

1. **Identity + scoped keys** (foundation; unblocks multi-tenant everything).
2. **Address registry** → reserved addresses + retention sweeper.
3. **MCP server** (read + `wait_for_message` + mint). High value, no sending.
4. **Forwarding** (relay seam + queue + worker + verification), or delegate to
   Cloudflare Email Routing.

Forwarding carries ~70% of the risk; modes 1–3 are low-risk receive-side
extensions and ship first.

## Open questions

- Password auth vs. OIDC for users — v1 ships local password (argon2/bcrypt);
  OIDC later.
- Per-key rate limiting — table is ready; enforcement deferred.
- ARC sealing to allow a forward banner without breaking DMARC — deferred.
