package smtp_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/smtp"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nees/zorail/internal/config"
	zsmtp "github.com/nees/zorail/internal/smtp"
	"github.com/nees/zorail/internal/storage/sqlite"
)

// TestEndToEnd boots the real SMTP server on an ephemeral port, sends a message
// with net/smtp, and verifies it was parsed and persisted to SQLite — the full
// MVP pipeline.
func TestEndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{
		Domain:          "zorail.test",
		MaxMessageBytes: 1024 * 1024,
		MaxRecipients:   10,
		AllowedDomains:  []string{"zorail.test"},
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := zsmtp.New(cfg, store, log, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	addr := ln.Addr().String()

	// Reject out-of-scope domains.
	if err := sendMail(addr, "sender@external.com", "nope@other.com",
		"Subject: x\r\n\r\nbody\r\n"); err == nil {
		t.Error("expected rejection for out-of-scope recipient domain")
	}

	// Accept in-scope mail.
	body := "Subject: Verify\r\n" +
		"From: app@external.com\r\n" +
		"To: signup-123@zorail.test\r\n" +
		"\r\n" +
		"Your verification code is 778899.\r\n"
	if err := sendMail(addr, "app@external.com", "signup-123@zorail.test", body); err != nil {
		t.Fatalf("send mail: %v", err)
	}

	// Give the server a moment to persist.
	deadline := time.Now().Add(3 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		msgs, err := store.ListMessages(context.Background(), "signup-123@zorail.test", 10, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		count = len(msgs)
		if count > 0 {
			full, err := store.GetMessage(context.Background(), msgs[0].ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if !strings.Contains(full.Text, "778899") {
				t.Errorf("stored body missing code: %q", full.Text)
			}
			if full.Subject != "Verify" {
				t.Errorf("subject = %q", full.Subject)
			}
			if full.Inbox != "signup-123@zorail.test" {
				t.Errorf("inbox = %q", full.Inbox)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("message was not stored (count=%d)", count)
}

func sendMail(addr, from, to, body string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
