// Package smtp wires Zorail's inbound SMTP server: it accepts mail for any
// allowed recipient, parses it, and hands it to storage. It never relays.
package smtp

import (
	"crypto/tls"
	"log/slog"
	"net"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/ingest"
)

// Server is the inbound SMTP server.
type Server struct {
	srv *gosmtp.Server
	log *slog.Logger
}

// New builds an SMTP server bound to cfg, handing received mail to ing. Pass a
// non-nil tlsConfig to enable STARTTLS; nil means plaintext only (fine behind a
// TLS terminator or for local testing).
func New(cfg *config.Config, ing *ingest.Service, log *slog.Logger, tlsConfig *tls.Config) *Server {
	be := &Backend{ing: ing, cfg: cfg, log: log}

	s := gosmtp.NewServer(be)
	s.Addr = cfg.SMTPAddr
	s.Domain = cfg.Domain
	s.ReadTimeout = 60 * time.Second
	s.WriteTimeout = 60 * time.Second
	s.MaxMessageBytes = cfg.MaxMessageBytes
	s.MaxRecipients = cfg.MaxRecipients
	// This is a receive-only sink; there are no user accounts to authenticate.
	s.AllowInsecureAuth = true
	if tlsConfig != nil {
		s.TLSConfig = tlsConfig
	}

	return &Server{srv: s, log: log}
}

// ListenAndServe blocks serving SMTP until Close is called or a fatal error
// occurs.
func (s *Server) ListenAndServe() error {
	s.log.Info("smtp listening", "addr", s.srv.Addr, "domain", s.srv.Domain)
	return s.srv.ListenAndServe()
}

// Serve serves SMTP on an already-open listener. Useful for tests (bind to
// 127.0.0.1:0 and read back the chosen port) and for socket-activated setups.
func (s *Server) Serve(l net.Listener) error {
	s.log.Info("smtp listening", "addr", l.Addr().String(), "domain", s.srv.Domain)
	return s.srv.Serve(l)
}

// Close gracefully shuts the server down.
func (s *Server) Close() error {
	return s.srv.Close()
}
