.PHONY: build run dev ui ui-install test tidy clean docker send-test

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

# Run the Go server: SMTP on :1025, web UI + API on :8090, catch-all domain.
# (8090 rather than the container default 8080 to dodge local port clashes.)
run:
	ZORAIL_SMTP_ADDR=:1025 ZORAIL_HTTP_ADDR=:8090 ZORAIL_DOMAIN=localhost go run ./cmd/zorail

# Frontend hot-reload dev server (proxies /api -> :8090). Run `make run` in a
# second terminal so the API is live, then open http://localhost:3000.
dev:
	cd $(UI_DIR) && pnpm dev

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
