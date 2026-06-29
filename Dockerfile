# --- Stage 1: build the Nuxt SPA into static files -------------------------
FROM node:22-alpine AS ui
RUN corepack enable
WORKDIR /ui
COPY ui/package.json ui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ui/ ./
RUN pnpm generate   # -> /ui/.output/public

# --- Stage 2: build the Go binary, embedding the freshly-built UI ----------
FROM --platform=$BUILDPLATFORM golang:1.25 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace the committed UI bundle with the one just built from source, so the
# image always matches ui/. go:embed (internal/api/embed.go) bakes it into the
# binary at compile time — one artifact contains API + UI.
RUN rm -rf internal/api/web
COPY --from=ui /ui/.output/public internal/api/web
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/zorail ./cmd/zorail

# --- Stage 3: minimal runtime --------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /data
COPY --from=build /out/zorail /usr/local/bin/zorail

# Bind SMTP and the HTTP API/UI in-container; map them on the host as needed.
ENV ZORAIL_SMTP_ADDR=:25 \
    ZORAIL_HTTP_ADDR=:8080 \
    ZORAIL_DB_PATH=/data/zorail.db
EXPOSE 25 8080
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/zorail"]
