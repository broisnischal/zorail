#!/usr/bin/env bash
#
# Zorail installer — one command to stand up a self-hosted Zorail instance.
#
#   curl -fsSL https://raw.githubusercontent.com/broisnischal/zorail/main/install.sh | bash
#
# It will:
#   1. install Docker if it isn't already present (Linux, via get.docker.com),
#   2. ask for your mail domain + ports (or take them from the environment),
#   3. pull the Zorail image and run it with a persistent data volume,
#   4. print the dashboard URL — open it to finish first-run setup in the browser
#      (create your admin account + organization).
#
# Non-interactive install (CI / cloud-init): set ZORAIL_NONINTERACTIVE=1 and any
# of ZORAIL_DOMAIN, ZORAIL_ALLOWED_DOMAINS, ZORAIL_HTTP_PORT, ZORAIL_SMTP_PORT,
# ZORAIL_IMAGE, ZORAIL_DATA_VOLUME beforehand.
set -euo pipefail

# ---- pretty output -----------------------------------------------------------
if [ -t 1 ]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; RED=$'\033[31m'; CYAN=$'\033[36m'; RESET=$'\033[0m'
else
  BOLD=""; DIM=""; GREEN=""; YELLOW=""; RED=""; CYAN=""; RESET=""
fi
info()  { printf '%s\n' "${CYAN}▸${RESET} $*"; }
ok()    { printf '%s\n' "${GREEN}✓${RESET} $*"; }
warn()  { printf '%s\n' "${YELLOW}!${RESET} $*"; }
die()   { printf '%s\n' "${RED}✗ $*${RESET}" >&2; exit 1; }

# ---- config (env overridable) ------------------------------------------------
IMAGE="${ZORAIL_IMAGE:-ghcr.io/broisnischal/zorail:latest}"
CONTAINER="${ZORAIL_CONTAINER:-zorail}"
DATA_VOLUME="${ZORAIL_DATA_VOLUME:-zorail-data}"
DOMAIN="${ZORAIL_DOMAIN:-}"
ALLOWED="${ZORAIL_ALLOWED_DOMAINS:-}"
HTTP_PORT="${ZORAIL_HTTP_PORT:-8080}"
SMTP_PORT="${ZORAIL_SMTP_PORT:-25}"
NONINTERACTIVE="${ZORAIL_NONINTERACTIVE:-}"

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 && SUDO="sudo" || true
fi

prompt() { # prompt VAR "question" "default"
  local __var="$1" __q="$2" __def="$3" __ans=""
  if [ -n "$NONINTERACTIVE" ] || [ ! -t 0 ]; then
    printf -v "$__var" '%s' "${!__var:-$__def}"; return
  fi
  read -r -p "$(printf '%s %s %s' "$__q" "${DIM}[$__def]${RESET}" '› ')" __ans || true
  printf -v "$__var" '%s' "${__ans:-${!__var:-$__def}}"
}

banner() {
  printf '\n%s\n' "${BOLD}  Zorail${RESET} ${DIM}· self-hosted disposable + forwarding mail${RESET}"
  printf '%s\n\n' "${DIM}  ────────────────────────────────────────────${RESET}"
}

# ---- 1. Docker ---------------------------------------------------------------
ensure_docker() {
  if command -v docker >/dev/null 2>&1; then
    ok "Docker present ($(docker --version | awk '{print $3}' | tr -d ,))"
  else
    info "Docker not found — installing…"
    local os; os="$(uname -s)"
    if [ "$os" != "Linux" ]; then
      die "Automatic Docker install is Linux-only. On macOS/Windows install Docker Desktop, then re-run this script."
    fi
    curl -fsSL https://get.docker.com | $SUDO sh || die "Docker installation failed."
    ok "Docker installed."
  fi
  # Make sure the daemon is up (systemd hosts).
  if ! docker info >/dev/null 2>&1; then
    if command -v systemctl >/dev/null 2>&1; then
      info "Starting Docker daemon…"
      $SUDO systemctl enable --now docker >/dev/null 2>&1 || true
    fi
  fi
  docker info >/dev/null 2>&1 || die "Docker daemon is not reachable. Start Docker and re-run."
}

# ---- 2. config ---------------------------------------------------------------
gather_config() {
  prompt DOMAIN  "Mail domain (used for generated addresses)" "${DOMAIN:-mail.example.com}"
  # Empty = accept mail for EVERY recipient domain (open catch-all). This is the
  # default: any address whose MX points here is accepted. Set a comma-separated
  # list only if you want to restrict which domains this server will receive for.
  prompt ALLOWED "Restrict to domains (comma-separated, blank = accept all)" "${ALLOWED:-}"
  prompt HTTP_PORT "Dashboard / API port" "$HTTP_PORT"
  prompt SMTP_PORT "Inbound SMTP port" "$SMTP_PORT"
}

# ---- 3. run ------------------------------------------------------------------
run_container() {
  info "Pulling ${IMAGE}…"
  docker pull "$IMAGE" >/dev/null || die "Could not pull $IMAGE. Set ZORAIL_IMAGE to a reachable image."

  if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    warn "Existing '$CONTAINER' container found — replacing it (data volume is preserved)."
    docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  fi

  docker volume create "$DATA_VOLUME" >/dev/null

  info "Starting Zorail…"
  # Only pin ZORAIL_ALLOWED_DOMAINS when the operator set one; leaving it unset
  # means accept-all (every recipient domain), which is the default.
  local allowed_args=()
  if [ -n "$ALLOWED" ]; then
    allowed_args=(-e "ZORAIL_ALLOWED_DOMAINS=$ALLOWED")
  fi

  docker run -d \
    --name "$CONTAINER" \
    --restart unless-stopped \
    -p "${HTTP_PORT}:8080" \
    -p "${SMTP_PORT}:25" \
    -e ZORAIL_DOMAIN="$DOMAIN" \
    "${allowed_args[@]}" \
    -e ZORAIL_SMTP_ADDR=":25" \
    -e ZORAIL_HTTP_ADDR=":8080" \
    -e ZORAIL_DB_PATH="/data/zorail.db" \
    -e ZORAIL_RETENTION_DAYS="${ZORAIL_RETENTION_DAYS:-14}" \
    -v "${DATA_VOLUME}:/data" \
    "$IMAGE" >/dev/null || die "Failed to start the container. Check 'docker logs $CONTAINER'."
}

# ---- main --------------------------------------------------------------------
banner
ensure_docker
gather_config
run_container

# Best-effort reachable host for the closing message.
HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"; [ -z "${HOST_IP:-}" ] && HOST_IP="localhost"

cat <<EOF

$(ok "Zorail is running.")

  ${BOLD}Dashboard${RESET}   http://${HOST_IP}:${HTTP_PORT}
  ${BOLD}Open it${RESET}     to finish setup — create your admin account & organization.

  ${DIM}Mail domain${RESET}  ${DOMAIN}
  ${DIM}Accepting${RESET}    $([ -n "$ALLOWED" ] && printf '%s' "$ALLOWED" || printf 'all domains (open catch-all)')
  ${DIM}SMTP${RESET}         port ${SMTP_PORT}  (point your domain's MX record here)
  ${DIM}Data${RESET}         docker volume '${DATA_VOLUME}'  (survives upgrades)

  ${DIM}Logs${RESET}         docker logs -f ${CONTAINER}
  ${DIM}Update${RESET}       re-run this script (pulls the latest image, keeps data)
  ${DIM}Stop${RESET}         docker rm -f ${CONTAINER}

${DIM}Note: inbound mail on port 25 needs a public IP with 25 reachable, or
delegate the MX to Cloudflare Email Routing and push mail to POST /api/ingest.${RESET}
EOF
