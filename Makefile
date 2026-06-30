.PHONY: build run up dev docker-dev docker-dev-down ui ui-install test tidy clean docker send-test watch setup doctor

BIN     := bin/zorail
UI_DIR  := ui
WEB_OUT := internal/api/web

# Build the static Nuxt SPA and copy it into the Go embed directory.
# Run this whenever the frontend (ui/) changes.
ui:
	cd $(UI_DIR) && pnpm install --frozen-lockfile && pnpm generate
	rm -rf $(WEB_OUT)
	mkdir -p $(WEB_OUT)
	cp -r $(UI_DIR)/.output/public/. $(WEB_OUT)/

# Compile the single binary (embeds whatever is currently in $(WEB_OUT)).
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/zorail

# Run the Go server: SMTP on :1025, web UI + API on :8090. The server auto-loads
# .env, so ZORAIL_DOMAIN / ZORAIL_API_TOKEN written by `zorail setup` take effect
# here (ports stay pinned to :1025/:8090; 8090 dodges the container default 8080).
run:
	ZORAIL_SMTP_ADDR=:1025 ZORAIL_HTTP_ADDR=:8090 go run ./cmd/zorail

# After `make setup`: start the server AND the Cloudflare Tunnel together, with
# combined logs. One command, Ctrl+C stops both. This is the normal way to run.
up:
	go run ./cmd/zorail up

# Launch the live terminal inbox viewer against a running server (make run).
# Override the target with URL=… TOKEN=…  e.g.  make watch URL=https://mail.example.com
watch:
	go run ./cmd/zorail watch $(if $(URL),--url $(URL),) $(if $(TOKEN),--token $(TOKEN),)

# Connect a real domain's inbound mail to this server (Cloudflare automation).
#   make setup DOMAIN=example.com      (prompts for the Cloudflare API token)
setup:
	go run ./cmd/zorail setup $(if $(DOMAIN),--domain $(DOMAIN),)

# Verify the inbound mail pipeline end-to-end.
doctor:
	go run ./cmd/zorail doctor

# Frontend hot-reload dev server (proxies /api -> :8090). Run `make run` in a
# second terminal so the API is live, then open http://localhost:3000.
dev:
	cd $(UI_DIR) && pnpm dev

# Fully dockerized dev stack with live reload (no local Go/Node needed):
#   UI  http://localhost:3000 (Nuxt HMR) · API :8090 (air) · SMTP :1025.
# Source is bind-mounted; saving .go rebuilds the API, saving ui/ hot-reloads.
docker-dev:
	docker compose -f docker-compose.dev.yml up --build

docker-dev-down:
	docker compose -f docker-compose.dev.yml down

# Send a sample email to the locally running server (run `make run` first).
send-test:
	python3 scripts/send-test-mail.py

test:
	go test ./...

tidy:
	go mod tidy

docker:
	docker build -t zorail:latest .

clean:
	rm -rf bin *.db *.db-wal *.db-shm $(UI_DIR)/.output $(UI_DIR)/.nuxt
