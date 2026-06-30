package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nees/zorail/internal/api"
	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/forward"
	"github.com/nees/zorail/internal/ingest"
	zmcp "github.com/nees/zorail/internal/mcp"
	"github.com/nees/zorail/internal/notify"
	zsmtp "github.com/nees/zorail/internal/smtp"
	"github.com/nees/zorail/internal/storage/sqlite"
)

// serve runs the all-in-one Zorail server: inbound SMTP ingest, the JSON API,
// the embedded web UI, an MCP server for agents, a forwarding worker, and a
// retention sweeper — all from one process over one config and one SQLite file.
func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg.LogLevel)
	log.Info("starting zorail",
		"smtp_addr", cfg.SMTPAddr,
		"domain", cfg.Domain,
		"db", cfg.DBPath,
		"allowed_domains", cfg.AllowedDomains,
		"relay", cfg.RelayEnabled(),
		"retention_days", cfg.RetentionDays,
	)

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// Background context for workers; cancelled on shutdown.
	bg, cancelBG := context.WithCancel(context.Background())
	defer cancelBG()

	// Shared collaborators.
	hub := notify.NewHub()
	ing := ingest.New(cfg, store, log, hub)
	mcpHandler := zmcp.NewManager(cfg, store, hub).Handler()

	// Forwarding relay (nil when no relay configured → forwarding delegated).
	relay := forward.NewSMTPRelay(cfg)
	var mailer api.Mailer
	if relay != nil {
		mailer = relay
	}
	go forward.NewWorker(cfg, store, relay, log).Run(bg)

	// Retention sweeper.
	if cfg.RetentionDays > 0 {
		go runSweeper(bg, store, log, cfg.RetentionDays)
	}

	smtpSrv := zsmtp.New(cfg, ing, log, nil)
	httpSrv, err := api.New(cfg, store, log, &api.Deps{
		Ingest: ing,
		Hub:    hub,
		Mailer: mailer,
		MCP:    mcpHandler,
	})
	if err != nil {
		return err
	}

	// Run both servers; the first fatal error wins.
	errCh := make(chan error, 2)
	go func() { errCh <- smtpSrv.ListenAndServe() }()
	go func() { errCh <- httpSrv.ListenAndServe() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, stopping")
		cancelBG()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutCtx); err != nil {
			log.Error("http shutdown", "err", err)
		}
		if err := smtpSrv.Close(); err != nil {
			log.Error("smtp close", "err", err)
		}
		return nil
	}
}

// runSweeper periodically deletes expired disposable mail (reserved/forward
// addresses are exempt — see storage.ExpireMessages).
func runSweeper(ctx context.Context, store *sqlite.Store, log *slog.Logger, retentionDays int) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	sweep := func() {
		cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
		n, err := store.ExpireMessages(ctx, cutoff)
		if err != nil {
			log.Error("retention sweep", "err", err)
			return
		}
		if n > 0 {
			log.Info("retention sweep", "deleted", n, "cutoff", cutoff)
		}
	}
	sweep() // run once at startup
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
