// Package config loads Zorail's runtime configuration from environment
// variables. Everything has a sensible default so `zorail` runs out of the box
// for local testing, while production deployments override via env.
package config

import (
	"bufio"
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

	// Retention. Disposable inboxes older than this are swept. Zero disables
	// sweeping; reserved/forward addresses are always exempt.
	RetentionDays int

	// Forwarding relay (smarthost). When RelayHost is set, the forward worker
	// delivers queued messages through it. Leave empty to delegate forwarding
	// to Cloudflare Email Routing (Zorail then only stores copies).
	RelayHost       string // relay hostname (e.g. smtp.resend.com)
	RelayPort       int    // relay port (587 for STARTTLS submission)
	RelayUser       string // relay username (empty = no auth)
	RelayPass       string // relay password
	RelayFrom       string // envelope MAIL FROM for forwards (SRS-lite); defaults to bounces@Domain
	ForwardMaxTries int    // give up after this many delivery attempts

	// LogLevel: "debug" | "info" | "warn" | "error".
	LogLevel string
}

// RelayEnabled reports whether an outbound relay is configured.
func (c *Config) RelayEnabled() bool { return c.RelayHost != "" }

// BounceFrom returns the envelope sender used for forwarded mail.
func (c *Config) BounceFrom() string {
	if c.RelayFrom != "" {
		return c.RelayFrom
	}
	d := c.Domain
	if len(c.AllowedDomains) > 0 {
		d = c.AllowedDomains[0]
	}
	return "bounces@" + d
}

// Load reads configuration from the environment, applying defaults. A `.env`
// file in the working directory (or the path in ZORAIL_ENV_FILE) is loaded
// first, so `ZORAIL_DOMAIN=…` in a file works without exporting it. Real
// environment variables always win over the file.
func Load() (*Config, error) {
	loadDotEnv(env("ZORAIL_ENV_FILE", ".env"))

	c := &Config{
		SMTPAddr:        env("ZORAIL_SMTP_ADDR", ":1025"),
		Domain:          env("ZORAIL_DOMAIN", "localhost"),
		MaxMessageBytes: envInt64("ZORAIL_MAX_MESSAGE_BYTES", 25*1024*1024), // 25 MiB
		MaxRecipients:   int(envInt64("ZORAIL_MAX_RECIPIENTS", 100)),
		AllowedDomains:  normalizeDomains(envList("ZORAIL_ALLOWED_DOMAINS")),
		HTTPAddr:        env("ZORAIL_HTTP_ADDR", ":8080"),
		APIToken:        env("ZORAIL_API_TOKEN", ""),
		DBPath:          env("ZORAIL_DB_PATH", "zorail.db"),
		RetentionDays:   int(envInt64("ZORAIL_RETENTION_DAYS", 0)),
		RelayHost:       env("ZORAIL_RELAY_HOST", ""),
		RelayPort:       int(envInt64("ZORAIL_RELAY_PORT", 587)),
		RelayUser:       env("ZORAIL_RELAY_USER", ""),
		RelayPass:       env("ZORAIL_RELAY_PASS", ""),
		RelayFrom:       env("ZORAIL_RELAY_FROM", ""),
		ForwardMaxTries: int(envInt64("ZORAIL_FORWARD_MAX_TRIES", 5)),
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

// loadDotEnv reads simple KEY=VALUE lines from path (if it exists) and sets any
// keys not already present in the environment. Lines may be blank, `# comments`,
// or `export KEY=value`; values may be single/double quoted. It is intentionally
// dependency-free and forgiving — a malformed line is skipped, never fatal.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // no file → nothing to do
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
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
