package smtp

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/id"
	"github.com/nees/zorail/internal/mailparse"
	"github.com/nees/zorail/internal/storage"
)

// Backend implements gosmtp.Backend, creating one Session per connection.
type Backend struct {
	store storage.Store
	cfg   *config.Config
	log   *slog.Logger
}

// NewSession is called once per inbound connection.
func (b *Backend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	remote := ""
	if c != nil && c.Conn() != nil {
		remote = c.Conn().RemoteAddr().String()
	}
	return &Session{be: b, remote: remote}, nil
}

// Session holds the state of a single SMTP transaction. go-smtp calls Mail,
// Rcpt (possibly many), then Data; Reset may clear state for a new message on
// the same connection.
type Session struct {
	be     *Backend
	remote string
	from   string
	rcpts  []string
}

// Mail records the envelope sender (MAIL FROM).
func (s *Session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

// Rcpt records a recipient (RCPT TO) if its domain is in scope.
func (s *Session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if !s.be.cfg.AllowsRecipient(to) {
		return &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 1, 1},
			Message:      "Recipient domain not handled by this server",
		}
	}
	s.rcpts = append(s.rcpts, to)
	return nil
}

// Data reads the message body, parses it once, and persists one copy per
// recipient. The reader must be fully consumed before returning.
func (s *Session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		s.be.log.Error("read DATA", "err", err, "remote", s.remote)
		return &gosmtp.SMTPError{Code: 451, Message: "Failed to read message"}
	}

	parsed, _ := mailparse.Parse(raw) // always returns a usable message
	receivedAt := time.Now().UTC()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, rcpt := range s.rcpts {
		m := parsed.Clone()
		m.ID = id.New()
		m.Inbox = normalizeInbox(rcpt)
		m.EnvFrom = s.from
		m.ReceivedAt = receivedAt

		if err := s.be.store.SaveMessage(ctx, m); err != nil {
			s.be.log.Error("save message", "err", err, "inbox", m.Inbox, "remote", s.remote)
			return &gosmtp.SMTPError{Code: 451, Message: "Failed to store message"}
		}
		s.be.log.Info("stored message",
			"id", m.ID,
			"inbox", m.Inbox,
			"from", firstNonEmpty(m.From, m.EnvFrom),
			"subject", m.Subject,
			"size", m.Size,
			"attachments", len(m.Attachments),
		)
	}

	return nil
}

// Reset clears per-message state so the connection can send another message.
func (s *Session) Reset() {
	s.from = ""
	s.rcpts = nil
}

// Logout frees connection-scoped resources (none today).
func (s *Session) Logout() error { return nil }

// normalizeInbox lowercases the recipient address so inbox lookups are
// case-insensitive, as is conventional for the domain part (and pragmatic for
// the local part in a temp-mail context).
func normalizeInbox(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
