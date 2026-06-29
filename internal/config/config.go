// Package config loads Zorail's runtime configuration from environment
// variables. Everything has a sensible default so `zorail` runs out of the box
// for local testing, while production deployments override via env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all tunables for the ingest + storage layers. Later phases
// (API server, AI providers) will extend this struct.
type Config struct {
	// SMTP listener.
	SMTPAddr        string // host:port to bind, e.g. ":1025" (dev) or ":25" (prod)
	Domain          string // SMTP greeting/HELO domain
	MaxMessageBytes int64  // reject DATA larger than this
	MaxRecipients   int    // max RCPT per transaction

	// AllowedDomains restricts which recipient domains this server will accept
	// mail for (the catch-all scope). Empty means accept every domain — handy
	// for local testing, but set it in production so you are not an open sink.
	AllowedDomains []string

	// HTTP API + web UI.
	HTTPAddr string // host:port for the JSON API and bundled web UI
	APIToken string // optional bearer token; when set, /api requires it

	// Storage.
	DBPath string // path to the SQLite database file

	// LogLevel: "debug" | "info" | "warn" | "error".
	LogLevel string
}

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	c := &Config{
		SMTPAddr:        env("ZORAIL_SMTP_ADDR", ":1025"),
		Domain:          env("ZORAIL_DOMAIN", "localhost"),
		MaxMessageBytes: envInt64("ZORAIL_MAX_MESSAGE_BYTES", 25*1024*1024), // 25 MiB
		MaxRecipients:   int(envInt64("ZORAIL_MAX_RECIPIENTS", 100)),
		AllowedDomains:  normalizeDomains(envList("ZORAIL_ALLOWED_DOMAINS")),
		HTTPAddr:        env("ZORAIL_HTTP_ADDR", ":8080"),
		APIToken:        env("ZORAIL_API_TOKEN", ""),
		DBPath:          env("ZORAIL_DB_PATH", "zorail.db"),
		LogLevel:        env("ZORAIL_LOG_LEVEL", "info"),
	}
	if c.MaxMessageBytes <= 0 {
		return nil, fmt.Errorf("ZORAIL_MAX_MESSAGE_BYTES must be positive")
	}
	return c, nil
}

// AllowsRecipient reports whether mail addressed to `addr` should be accepted.
// When AllowedDomains is empty, every recipient is accepted (open catch-all).
func (c *Config) AllowsRecipient(addr string) bool {
	if len(c.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndexByte(addr, '@')
	if at < 0 {
		return false
	}
	domain := strings.ToLower(addr[at+1:])
	for _, d := range c.AllowedDomains {
		if d == domain {
			return true
		}
	}
	return false
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envList(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeDomains(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		out = append(out, strings.ToLower(strings.TrimSpace(d)))
	}
	return out
}
