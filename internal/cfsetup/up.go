package cfsetup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Up runs the two local processes that make inbound mail flow — the Zorail
// server and the Cloudflare Tunnel — from the configuration `zorail setup`
// wrote. It streams both logs with prefixes and tears both down on Ctrl+C or
// when either one exits.
func Up(ctx context.Context, o Options) error {
	if o.EnvFile == "" {
		o.EnvFile = RepoEnvFile()
	}
	repoRoot := filepath.Dir(o.EnvFile)

	// Tunnel run token: prefer the one setup persisted; fall back to the API.
	tunTok := readEnvValue(o.EnvFile, "ZORAIL_TUNNEL_TOKEN")
	if tunTok == "" {
		t, err := tunnelTokenFromAPI(ctx, o)
		if err != nil {
			return fmt.Errorf("no ZORAIL_TUNNEL_TOKEN in %s and couldn't fetch one (%v) — run `zorail setup` first", o.EnvFile, err)
		}
		tunTok = t
	}

	cfdPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared is not installed — get it from https://pkg.cloudflare.com/ (macOS: `brew install cloudflared`), then re-run `zorail up`")
	}

	fmt.Println(bold("\n  zorail up — starting Zorail server + Cloudflare Tunnel"))
	fmt.Printf("  config %s · Ctrl+C stops both\n", o.EnvFile)

	// A child context so that when one process exits we can cancel the other;
	// exec.CommandContext kills the process when its context is cancelled.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv := serverCmd(runCtx, repoRoot)
	srv.Stdout, srv.Stderr = prefixWriter("server", os.Stdout), prefixWriter("server", os.Stderr)

	tun := exec.CommandContext(runCtx, cfdPath, "tunnel", "run", "--token", tunTok)
	tun.Stdout, tun.Stderr = prefixWriter("tunnel", os.Stdout), prefixWriter("tunnel", os.Stderr)

	if err := srv.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	if err := tun.Start(); err != nil {
		cancel()
		_ = srv.Wait()
		return fmt.Errorf("start cloudflared: %w", err)
	}
	okf("server pid %d · tunnel pid %d", srv.Process.Pid, tun.Process.Pid)

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); errCh <- tagErr("server", srv.Wait()) }()
	go func() { defer wg.Done(); errCh <- tagErr("tunnel", tun.Wait()) }()

	select {
	case <-runCtx.Done(): // Ctrl+C
		fmt.Fprintln(os.Stderr, "\n  "+yellow("!")+" stopping…")
	case err := <-errCh: // one process died on its own
		fmt.Fprintln(os.Stderr, "\n  "+yellow("!")+" "+err.Error()+" — stopping the other")
	}
	cancel()
	wg.Wait()
	return nil
}

// serverCmd builds the command that runs the Zorail server, preferring a built
// `zorail` binary (next to this executable, then on PATH) and falling back to
// `go run ./cmd/zorail` for a source checkout. The server auto-loads ./.env, so
// Dir is set to the repo root where setup wrote it.
func serverCmd(ctx context.Context, repoRoot string) *exec.Cmd {
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "zorail")
		if isExecutable(cand) {
			c := exec.CommandContext(ctx, cand)
			c.Dir = repoRoot
			return c
		}
	}
	if p, err := exec.LookPath("zorail"); err == nil {
		c := exec.CommandContext(ctx, p)
		c.Dir = repoRoot
		return c
	}
	c := exec.CommandContext(ctx, "go", "run", "./cmd/zorail")
	c.Dir = repoRoot
	return c
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// tunnelTokenFromAPI fetches the tunnel run token from Cloudflare using the
// saved setup state and an available Cloudflare token.
func tunnelTokenFromAPI(ctx context.Context, o Options) (string, error) {
	st, err := LoadState()
	if err != nil {
		return "", fmt.Errorf("no saved setup found: %w", err)
	}
	token := o.CFToken
	if token == "" {
		token = os.Getenv("CLOUDFLARE_API_TOKEN")
	}
	if token == "" {
		token = readEnvValue(o.EnvFile, "CLOUDFLARE_API_TOKEN")
	}
	if token == "" {
		return "", fmt.Errorf("no Cloudflare token available")
	}
	return NewCF(token).TunnelRunToken(ctx, st.AccountID, st.TunnelID)
}

func tagErr(tag string, err error) error {
	if err == nil {
		return fmt.Errorf("%s exited", tag)
	}
	return fmt.Errorf("%s exited (%v)", tag, err)
}

// linePrefixer writes each complete line to w prefixed with a colored tag, so
// the interleaved server and tunnel logs stay readable.
type linePrefixer struct {
	prefix string
	w      io.Writer
	mu     sync.Mutex
	buf    []byte
}

func prefixWriter(tag string, w io.Writer) io.Writer {
	color := "\x1b[36m" // cyan: server
	if tag == "tunnel" {
		color = "\x1b[35m" // magenta: tunnel
	}
	return &linePrefixer{prefix: color + tag + "\x1b[0m | ", w: w}
}

func (l *linePrefixer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		_, _ = io.WriteString(l.w, l.prefix)
		_, _ = l.w.Write(l.buf[:i+1])
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}
