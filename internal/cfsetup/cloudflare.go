// Package cfsetup automates wiring a real domain's inbound mail into a
// localhost Zorail server: a Cloudflare Email Routing catch-all → an Email
// Worker → an HTTPS POST to /api/ingest, reached through a Cloudflare Tunnel.
// It is driven by `zorail setup` and verified by `zorail doctor`.
package cfsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const cfAPI = "https://api.cloudflare.com/client/v4"

// CF is a minimal Cloudflare REST client scoped to the calls this setup needs.
type CF struct {
	token string
	hc    *http.Client
}

func NewCF(token string) *CF {
	return &CF{token: strings.TrimSpace(token), hc: &http.Client{Timeout: 30 * time.Second}}
}

// envelope is the standard Cloudflare API response wrapper.
type envelope struct {
	Success  bool              `json:"success"`
	Errors   []cfErr           `json:"errors"`
	Result   json.RawMessage   `json:"result"`
	Messages []json.RawMessage `json:"messages"`
}

type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e cfErr) Error() string { return fmt.Sprintf("cloudflare error %d: %s", e.Code, e.Message) }

// call performs a JSON request and unmarshals .result into out (may be nil).
func (c *CF) call(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfAPI+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *CF) send(req *http.Request, out any) error {
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s %s: HTTP %d: %s", req.Method, req.URL.Path, res.StatusCode, truncate(string(raw), 200))
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			return fmt.Errorf("%s %s: %s", req.Method, req.URL.Path, joinErrs(env.Errors))
		}
		return fmt.Errorf("%s %s: HTTP %d (unspecified failure)", req.Method, req.URL.Path, res.StatusCode)
	}
	if out != nil && len(env.Result) > 0 && string(env.Result) != "null" {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func joinErrs(errs []cfErr) string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- token / account / zone ----

// ErrAccountOwnedToken signals that the supplied token is a valid *account-owned*
// Cloudflare token. Those cannot complete setup: Cloudflare's Tunnel API rejects
// them with "Authentication error (10000)". Callers should tell the user to
// create a user-owned token instead. Detecting this here — rather than letting
// tunnel creation fail cryptically several steps later — keeps setup honest.
var ErrAccountOwnedToken = errors.New("cloudflare token is account-owned")

// VerifyToken confirms the API token is a usable user-owned token before setup
// does real work. A user-owned token verifies at /user/tokens/verify and works
// across every endpoint setup uses. If that check fails, VerifyToken figures out
// whether the token is a valid-but-unusable account-owned token (returns
// ErrAccountOwnedToken) or simply invalid (returns a copy-check hint).
func (c *CF) VerifyToken(ctx context.Context) error {
	status, err := c.verifyAt(ctx, "/user/tokens/verify")
	if err == nil {
		return checkStatus(status)
	}

	// The user endpoint rejected it. Determine which failure this is by probing
	// the account-scoped verify (an account-owned token passes there): discover
	// the account from a visible zone (the token carries Zone:Read).
	if zones, zerr := c.ListZones(ctx); zerr == nil && len(zones) > 0 && zones[0].Account.ID != "" {
		if st, aerr := c.verifyAt(ctx, "/accounts/"+zones[0].Account.ID+"/tokens/verify"); aerr == nil && st == "active" {
			return ErrAccountOwnedToken
		}
	}
	return fmt.Errorf("could not verify the Cloudflare API token — check it was copied in full "+
		"(no leading/trailing characters) and has the listed permissions. Underlying error: %w", err)
}

// verifyAt calls a *tokens/verify endpoint and returns the reported status.
func (c *CF) verifyAt(ctx context.Context, path string) (string, error) {
	var r struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, http.MethodGet, path, nil, &r); err != nil {
		return "", err
	}
	return r.Status, nil
}

func checkStatus(status string) error {
	if status != "active" {
		return fmt.Errorf("API token status is %q (expected active)", status)
	}
	return nil
}

type Zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
}

// Probe performs a minimal read against an endpoint to test whether the token
// is authorized for it. nil means authorized; a non-nil error (typically an
// authorization failure) means it is not. Used by setup's permission preflight.
func (c *CF) Probe(ctx context.Context, path string) error {
	return c.call(ctx, http.MethodGet, path, nil, nil)
}

// Account is a Cloudflare account the token can access.
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AccessibleAccounts lists the accounts this token can act on. An empty result
// (with no error) means the token's Account Resources scope is empty or excludes
// the account you're targeting — the usual cause of "Authentication error 10000"
// on account-level endpoints even when zone reads work.
func (c *CF) AccessibleAccounts(ctx context.Context) ([]Account, error) {
	var accts []Account
	err := c.call(ctx, http.MethodGet, "/accounts?per_page=50", nil, &accts)
	return accts, err
}

// CanAccessAccount reports whether the token can act on accountID. The bool is
// only meaningful when err is nil; a non-nil err means the check itself failed
// (e.g. the token cannot even list accounts).
func (c *CF) CanAccessAccount(ctx context.Context, accountID string) (bool, error) {
	accts, err := c.AccessibleAccounts(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range accts {
		if a.ID == accountID {
			return true, nil
		}
	}
	return false, nil
}

// ListZones returns every zone the token can see, for interactive selection.
func (c *CF) ListZones(ctx context.Context) ([]Zone, error) {
	var zones []Zone
	err := c.call(ctx, http.MethodGet, "/zones?per_page=50&order=name", nil, &zones)
	return zones, err
}

// Zone resolves a domain to its Cloudflare zone, walking up parent labels so a
// subdomain (mm.lexicon.website) resolves to its registered zone (lexicon.website).
func (c *CF) Zone(ctx context.Context, domain string) (*Zone, error) {
	labels := strings.Split(strings.TrimSuffix(domain, "."), ".")
	// Stop before a bare TLD: try domain, then each parent down to second-level.
	for i := 0; i+1 < len(labels); i++ {
		candidate := strings.Join(labels[i:], ".")
		var zones []Zone
		if err := c.call(ctx, http.MethodGet, "/zones?name="+candidate, nil, &zones); err != nil {
			return nil, err
		}
		if len(zones) > 0 {
			return &zones[0], nil
		}
	}
	return nil, fmt.Errorf("no Cloudflare zone found for %q or any parent — add the domain to Cloudflare first", domain)
}

// ---- DNS ----

type DNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority *int   `json:"priority,omitempty"`
	Proxied  *bool  `json:"proxied,omitempty"`
	TTL      int    `json:"ttl,omitempty"`
}

func (c *CF) ListDNS(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	var recs []DNSRecord
	err := c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records?per_page=500", nil, &recs)
	return recs, err
}

// UpsertDNS creates a record, or updates the matching one (same type+name) so
// the operation is idempotent across repeated setup runs.
func (c *CF) UpsertDNS(ctx context.Context, zoneID string, rec DNSRecord) error {
	existing, err := c.ListDNS(ctx, zoneID)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Type, rec.Type) && strings.EqualFold(e.Name, rec.Name) && e.Content == rec.Content {
			return nil // already present and identical
		}
	}
	// CNAME is a singleton per name, so re-point an existing one in place (e.g.
	// a new tunnel ID). MX/TXT can legitimately coexist with the same name and
	// different content (Cloudflare adds three MX records), so just add those.
	if strings.EqualFold(rec.Type, "CNAME") {
		for _, e := range existing {
			if strings.EqualFold(e.Type, rec.Type) && strings.EqualFold(e.Name, rec.Name) {
				return c.call(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+e.ID, rec, nil)
			}
		}
	}
	return c.call(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", rec, nil)
}

// ---- Email Routing ----

type EmailRouting struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
	Name    string `json:"name"`
}

func (c *CF) GetEmailRouting(ctx context.Context, zoneID string) (*EmailRouting, error) {
	var r EmailRouting
	err := c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/email/routing", nil, &r)
	return &r, err
}

func (c *CF) EnableEmailRouting(ctx context.Context, zoneID string) error {
	return c.call(ctx, http.MethodPost, "/zones/"+zoneID+"/email/routing/enable", map[string]any{}, nil)
}

// StandardEmailRoutingDNS returns the MX + SPF records every Cloudflare Email
// Routing zone uses. They are identical for every zone, so setup writes them
// directly: the live GET /email/routing/dns endpoint rejects scoped API tokens
// with error 10000 regardless of the token's permissions. The record name is
// the full mail domain, which is correct for both an apex and a subdomain.
func StandardEmailRoutingDNS(domain string) []DNSRecord {
	mx := func(host string, prio int) DNSRecord {
		p := prio
		return DNSRecord{Type: "MX", Name: domain, Content: host, Priority: &p, TTL: 1}
	}
	return []DNSRecord{
		mx("route1.mx.cloudflare.net", 11),
		mx("route2.mx.cloudflare.net", 28),
		mx("route3.mx.cloudflare.net", 74),
		{Type: "TXT", Name: domain, Content: "v=spf1 include:_spf.mx.cloudflare.net ~all", TTL: 1},
	}
}

// EmailRoutingDNS returns the MX/TXT records Cloudflare requires for routing.
// When subdomain is non-empty (e.g. "mm"), it returns the records for that
// subdomain instead of the zone apex.
func (c *CF) EmailRoutingDNS(ctx context.Context, zoneID, subdomain string) ([]DNSRecord, error) {
	path := "/zones/" + zoneID + "/email/routing/dns"
	if subdomain != "" {
		path += "?subdomain=" + url.QueryEscape(subdomain)
	}
	var recs []DNSRecord
	err := c.call(ctx, http.MethodGet, path, nil, &recs)
	return recs, err
}

// SetCatchAllToWorker routes every address for the domain to the named Worker.
func (c *CF) SetCatchAllToWorker(ctx context.Context, zoneID, script string) error {
	body := map[string]any{
		"enabled":  true,
		"name":     "zorail catch-all",
		"matchers": []map[string]any{{"type": "all"}},
		"actions":  []map[string]any{{"type": "worker", "value": []string{script}}},
	}
	return c.call(ctx, http.MethodPut, "/zones/"+zoneID+"/email/routing/rules/catch_all", body, nil)
}

// CatchAllWorker returns the Worker script the catch-all rule targets, or ""
// if the catch-all is unset or routes elsewhere.
func (c *CF) CatchAllWorker(ctx context.Context, zoneID string) (string, bool, error) {
	var r struct {
		Enabled bool `json:"enabled"`
		Actions []struct {
			Type  string   `json:"type"`
			Value []string `json:"value"`
		} `json:"actions"`
	}
	if err := c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/email/routing/rules/catch_all", nil, &r); err != nil {
		return "", false, err
	}
	for _, a := range r.Actions {
		if a.Type == "worker" && len(a.Value) > 0 {
			return a.Value[0], r.Enabled, nil
		}
	}
	return "", r.Enabled, nil
}

// ---- Workers ----

type workerVar struct {
	Name   string
	Value  string
	Secret bool
}

// DeployEmailWorker uploads an ES-module Worker with the given source + vars.
// Email Workers need no special binding — the catch-all routing rule connects
// inbound mail to the script by name.
func (c *CF) DeployEmailWorker(ctx context.Context, accountID, name, script string, vars []workerVar) error {
	const mainModule = "worker.js"

	bindings := make([]map[string]any, 0, len(vars))
	for _, v := range vars {
		t := "plain_text"
		if v.Secret {
			t = "secret_text"
		}
		bindings = append(bindings, map[string]any{"type": t, "name": v.Name, "text": v.Value})
	}
	metadata := map[string]any{
		"main_module":        mainModule,
		"compatibility_date": "2025-06-01",
		"bindings":           bindings,
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	metaJSON, _ := json.Marshal(metadata)
	_ = mw.WriteField("metadata", string(metaJSON))

	// The module part's form-field name must equal main_module.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, mainModule, mainModule))
	h.Set("Content-Type", "application/javascript+module")
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write([]byte(script)); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		cfAPI+"/accounts/"+accountID+"/workers/scripts/"+name, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req, nil)
}

func (c *CF) WorkerExists(ctx context.Context, accountID, name string) bool {
	// Use the /settings subpath: it returns a JSON envelope, whereas the bare
	// script endpoint returns the multipart module body, which fails to parse.
	err := c.call(ctx, http.MethodGet, "/accounts/"+accountID+"/workers/scripts/"+name+"/settings", nil, nil)
	return err == nil
}

// ---- Tunnel ----

type Tunnel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"-"` // fetched separately
}

func (c *CF) FindTunnel(ctx context.Context, accountID, name string) (*Tunnel, error) {
	var tunnels []Tunnel
	if err := c.call(ctx, http.MethodGet,
		"/accounts/"+accountID+"/cfd_tunnel?name="+name+"&is_deleted=false", nil, &tunnels); err != nil {
		return nil, err
	}
	if len(tunnels) == 0 {
		return nil, nil
	}
	return &tunnels[0], nil
}

// TunnelRunToken returns the run token for an existing tunnel — the credential
// `cloudflared tunnel run --token` needs. Used by `zorail up` to start the
// tunnel without re-provisioning.
func (c *CF) TunnelRunToken(ctx context.Context, accountID, tunnelID string) (string, error) {
	var tok string
	err := c.call(ctx, http.MethodGet, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/token", nil, &tok)
	return tok, err
}

// EnsureTunnel finds or creates a remotely-managed (config_src: cloudflare)
// tunnel and returns it with its run token populated.
func (c *CF) EnsureTunnel(ctx context.Context, accountID, name string) (*Tunnel, error) {
	t, err := c.FindTunnel(ctx, accountID, name)
	if err != nil {
		return nil, err
	}
	if t == nil {
		t = &Tunnel{}
		body := map[string]any{"name": name, "config_src": "cloudflare"}
		if err := c.call(ctx, http.MethodPost, "/accounts/"+accountID+"/cfd_tunnel", body, t); err != nil {
			return nil, err
		}
	}
	var tok string
	if err := c.call(ctx, http.MethodGet, "/accounts/"+accountID+"/cfd_tunnel/"+t.ID+"/token", nil, &tok); err != nil {
		return nil, fmt.Errorf("fetch tunnel token: %w", err)
	}
	t.Token = tok
	return t, nil
}

// ConfigureTunnelIngress points hostname (optionally a single path) at the
// local origin and 404s everything else, minimizing public exposure.
func (c *CF) ConfigureTunnelIngress(ctx context.Context, accountID, tunnelID, hostname, path, origin string) error {
	ingress := []map[string]any{
		{"hostname": hostname, "path": path, "service": origin},
		{"service": "http_status:404"},
	}
	body := map[string]any{"config": map[string]any{"ingress": ingress}}
	return c.call(ctx, http.MethodPut,
		"/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", body, nil)
}

type TunnelConn struct {
	Connections []struct {
		ID string `json:"id"`
	} `json:"conns"`
}

// TunnelHealthy reports whether the tunnel currently has active connections
// (i.e. a cloudflared instance is running and registered).
func (c *CF) TunnelHealthy(ctx context.Context, accountID, tunnelID string) (bool, error) {
	var t struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, http.MethodGet, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID, nil, &t); err != nil {
		return false, err
	}
	return t.Status == "healthy" || t.Status == "degraded", nil
}
