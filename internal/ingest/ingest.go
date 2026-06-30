// Package ingest holds the one place a received message is fanned out to its
// recipients, persisted, and side-effected (wake long-poll waiters, enqueue
// forwards). Both the SMTP backend and the HTTP /api/ingest endpoint call
// Accept, so the two entry points behave identically.
package ingest

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/id"
	"github.com/nees/zorail/internal/mailparse"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/notify"
	"github.com/nees/zorail/internal/storage"
)

// Service performs parse → fan-out → store, plus arrival notification and
// forward enqueue. The notify hub is optional (nil disables wake-ups).
type Service struct {
	store storage.Store
	cfg   *config.Config
	log   *slog.Logger
	hub   *notify.Hub
}

// New constructs an ingest Service. hub may be nil.
func New(cfg *config.Config, store storage.Store, log *slog.Logger, hub *notify.Hub) *Service {
	return &Service{store: store, cfg: cfg, log: log, hub: hub}
}

// Accept parses raw once and stores one copy per accepted recipient. It returns
// the number of stored copies. Recipients whose domain is out of scope are
// skipped silently (the SMTP layer rejects them earlier at RCPT; this guards
// the HTTP path). A nil/zero error means every in-scope recipient was stored.
func (s *Service) Accept(ctx context.Context, raw []byte, envFrom string, rcpts []string) (int, error) {
	parsed, _ := mailparse.Parse(raw) // always returns a usable message
	receivedAt := time.Now().UTC()

	stored := 0
	for _, rcpt := range rcpts {
		inbox := normalizeInbox(rcpt)
		if inbox == "" || !s.cfg.AllowsRecipient(inbox) {
			continue
		}

		m := parsed.Clone()
		m.ID = id.New()
		m.Inbox = inbox
		m.EnvFrom = envFrom
		m.ReceivedAt = receivedAt

		if err := s.store.SaveMessage(ctx, m); err != nil {
			return stored, err
		}
		stored++
		s.log.Info("stored message",
			"id", m.ID, "inbox", m.Inbox,
			"from", firstNonEmpty(m.From, m.EnvFrom), "subject", m.Subject,
			"size", m.Size, "attachments", len(m.Attachments))

		if s.hub != nil {
			s.hub.Publish(m.Inbox, m.ID)
		}
		s.maybeForward(ctx, m)
	}
	return stored, nil
}

// maybeForward enqueues delivery jobs when the recipient is a forwarding
// address with forwarding enabled and at least one verified destination.
func (s *Service) maybeForward(ctx context.Context, m *model.Message) {
	addr, err := s.store.GetAddress(ctx, m.Inbox)
	if err != nil {
		return // no registry row (plain disposable/reserved) or lookup failed
	}
	if addr.Type != model.AddrForward || !addr.ForwardEnabled {
		return
	}
	for _, dest := range addr.ForwardTo {
		ok, err := s.store.IsVerified(ctx, dest)
		if err != nil || !ok {
			if !ok {
				s.log.Warn("skipping forward to unverified destination", "src", m.Inbox, "dest", dest)
			}
			continue
		}
		job := &model.ForwardJob{
			ID:            id.New(),
			MessageID:     m.ID,
			SrcAddress:    m.Inbox,
			Dest:          dest,
			Raw:           m.Raw,
			Status:        model.ForwardPending,
			NextAttemptAt: time.Now().UTC(),
			CreatedAt:     time.Now().UTC(),
		}
		if err := s.store.EnqueueForward(ctx, job); err != nil {
			s.log.Error("enqueue forward", "err", err, "src", m.Inbox, "dest", dest)
		}
	}
}

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
