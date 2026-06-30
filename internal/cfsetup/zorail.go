package cfsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// zorail is a tiny client for the local Zorail API — just what setup/doctor need.
type zorail struct {
	base  string
	token string
	hc    *http.Client
}

func newZorail(base, token string) *zorail {
	base = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(base), "/"), "/api")
	if !strings.HasPrefix(base, "http") {
		base = "http://" + base
	}
	return &zorail{base: base, token: strings.TrimSpace(token), hc: &http.Client{Timeout: 15 * time.Second}}
}

type zorailConfig struct {
	Version      string `json:"version"`
	Domain       string `json:"domain"`
	AuthRequired bool   `json:"auth_required"`
}

func (z *zorail) config(ctx context.Context) (*zorailConfig, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, z.base+"/api/config", nil)
	res, err := z.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var c zorailConfig
	if err := json.NewDecoder(res.Body).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// check verifies the token is accepted (read scope) by listing inboxes.
func (z *zorail) check(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, z.base+"/api/inboxes", nil)
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	res, err := z.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// messageCount returns how many messages an inbox currently holds.
func (z *zorail) messageCount(ctx context.Context, inbox string) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, z.base+"/api/inboxes/"+url.PathEscape(inbox)+"/messages", nil)
	if z.token != "" {
		req.Header.Set("Authorization", "Bearer "+z.token)
	}
	res, err := z.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	var msgs []json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&msgs); err != nil {
		return 0, err
	}
	return len(msgs), nil
}

// postIngest pushes a raw RFC822 message to the *public* ingress URL, exactly
// as the Email Worker would — used by doctor for a true end-to-end probe.
func postIngest(ctx context.Context, ingestURL, token, rcpt, envFrom string, raw []byte) error {
	u, err := url.Parse(ingestURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("rcpt", rcpt)
	q.Set("env_from", envFrom)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set("Authorization", "Bearer "+token)

	hc := &http.Client{Timeout: 20 * time.Second}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
