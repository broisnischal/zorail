package smtp

import (
	"context"
	"io"
	"log/slog"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/ingest"
)

// Backend implements gosmtp.Backend, creating one Session per connection. It
// delegates all parse/store/side-effect work to the shared ingest service so
// SMTP and HTTP ingest behave identically.
type Backend struct {
	ing *ingest.Service
	cfg *config.Config
	log *slog.Logger
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

// Data reads the message body and hands it to the ingest service, which parses
// once and persists one copy per recipient.
func (s *Session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		s.be.log.Error("read DATA", "err", err, "remote", s.remote)
		return &gosmtp.SMTPError{Code: 451, Message: "Failed to read message"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := s.be.ing.Accept(ctx, raw, s.from, s.rcpts); err != nil {
		s.be.log.Error("ingest", "err", err, "remote", s.remote)
		return &gosmtp.SMTPError{Code: 451, Message: "Failed to store message"}
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
