// Command zorail is the self-hosted disposable-inbox server. This MVP runs the
// inbound SMTP ingest + storage pipeline; the API, dashboard, and AI layers
// build on the same config and storage in later phases.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"time"

	"github.com/nees/zorail/internal/api"
	"github.com/nees/zorail/internal/config"
	zsmtp "github.com/nees/zorail/internal/smtp"
	"github.com/nees/zorail/internal/storage/sqlite"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
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
	)

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	smtpSrv := zsmtp.New(cfg, store, log, nil)
	httpSrv, err := api.New(cfg, store, log)
	if err != nil {
		return err
	}

	// Run both servers; the first fatal error wins.
	errCh := make(chan error, 2)
	go func() { errCh <- smtpSrv.ListenAndServe() }()
	go func() { errCh <- httpSrv.ListenAndServe() }()

	// Wait for a fatal server error or a shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, stopping")
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
