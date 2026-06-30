// Package forward delivers received mail to external destinations. Zorail never
// operates raw outbound SMTP with its own IP reputation; instead it relays
// through a configured smarthost (a transactional provider or any submission
// server). Messages are re-emitted verbatim so the original DKIM signature
// survives, and the envelope sender is rewritten to a Zorail-owned bounce
// address (SRS-lite) so SPF aligns on the forwarding hop.
package forward

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"

	"github.com/nees/zorail/internal/config"
)

// Sender delivers one already-composed RFC 5322 message to its recipients.
type Sender interface {
	Send(ctx context.Context, from string, to []string, raw []byte) error
}

// SMTPRelay is a Sender backed by a submission relay (host:port, optional auth,
// STARTTLS when offered).
type SMTPRelay struct {
	host string
	port int
	auth smtp.Auth
}

// NewSMTPRelay builds a relay Sender from config, or returns nil when no relay
// is configured (forwarding then relies on Cloudflare Email Routing instead).
func NewSMTPRelay(cfg *config.Config) *SMTPRelay {
	if !cfg.RelayEnabled() {
		return nil
	}
	var auth smtp.Auth
	if cfg.RelayUser != "" {
		auth = smtp.PlainAuth("", cfg.RelayUser, cfg.RelayPass, cfg.RelayHost)
	}
	return &SMTPRelay{host: cfg.RelayHost, port: cfg.RelayPort, auth: auth}
}

// Send delivers raw to the recipients through the relay. It speaks SMTP
// directly (rather than smtp.SendMail) so it can honor the context deadline and
// opportunistically STARTTLS.
func (r *SMTPRelay) Send(ctx context.Context, from string, to []string, raw []byte) error {
	addr := net.JoinHostPort(r.host, fmt.Sprintf("%d", r.port))
	d := net.Dialer{}
	if dl, ok := ctx.Deadline(); ok {
		d.Deadline = dl
	} else {
		d.Timeout = 30 * time.Second
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	c, err := smtp.NewClient(conn, r.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: r.host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if r.auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(r.auth); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := wc.Write(raw); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return c.Quit()
}
