package forward

import (
	"context"
	"log/slog"
	"time"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/storage"
)

// Worker drains the forward queue, delivering each job through the Sender with
// exponential backoff and a maximum attempt count.
type Worker struct {
	store    storage.Store
	sender   Sender
	cfg      *config.Config
	log      *slog.Logger
	interval time.Duration
	batch    int
}

// NewWorker builds a forward worker. sender may be nil, in which case Run is a
// no-op (forwarding is delegated elsewhere, e.g. Cloudflare Email Routing).
func NewWorker(cfg *config.Config, store storage.Store, sender Sender, log *slog.Logger) *Worker {
	return &Worker{
		store:    store,
		sender:   sender,
		cfg:      cfg,
		log:      log,
		interval: 5 * time.Second,
		batch:    10,
	}
}

// Run polls the queue until ctx is cancelled. Safe to call in a goroutine.
func (wk *Worker) Run(ctx context.Context) {
	if wk.sender == nil {
		wk.log.Info("forward worker disabled (no relay configured)")
		return
	}
	wk.log.Info("forward worker started", "interval", wk.interval.String())
	t := time.NewTicker(wk.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			wk.drain(ctx)
		}
	}
}

func (wk *Worker) drain(ctx context.Context) {
	jobs, err := wk.store.ClaimForwardJobs(ctx, time.Now().UTC(), wk.batch)
	if err != nil {
		wk.log.Error("claim forward jobs", "err", err)
		return
	}
	for _, j := range jobs {
		sendCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := wk.sender.Send(sendCtx, wk.cfg.BounceFrom(), []string{j.Dest}, j.Raw)
		cancel()
		if err == nil {
			if err := wk.store.MarkForwardSent(ctx, j.ID); err != nil {
				wk.log.Error("mark forward sent", "err", err, "job", j.ID)
			}
			wk.log.Info("forwarded message", "src", j.SrcAddress, "dest", j.Dest, "attempts", j.Attempts)
			continue
		}

		if j.Attempts >= wk.cfg.ForwardMaxTries {
			wk.log.Error("forward permanently failed", "dest", j.Dest, "attempts", j.Attempts, "err", err)
			_ = wk.store.MarkForwardFailed(ctx, j.ID, err.Error())
			continue
		}
		next := time.Now().UTC().Add(backoff(j.Attempts))
		wk.log.Warn("forward failed, will retry", "dest", j.Dest, "attempts", j.Attempts, "next", next, "err", err)
		_ = wk.store.MarkForwardRetry(ctx, j.ID, j.Attempts, next, err.Error())
	}
}

// backoff returns an escalating delay: ~1m, 4m, 9m, ... capped at 1h.
func backoff(attempt int) time.Duration {
	d := time.Duration(attempt*attempt) * time.Minute
	if d < time.Minute {
		d = time.Minute
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}
