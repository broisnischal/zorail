// Package api serves Zorail's JSON API and the bundled web UI over HTTP. The UI
// is embedded into the binary (see embed.go), so this one server is the whole
// user-facing surface.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/nees/zorail/internal/config"
	"github.com/nees/zorail/internal/extract"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// Version is the advertised server version, surfaced via /api/config.
const Version = "0.2.0"

// Server is the HTTP server for the API + UI.
type Server struct {
	srv   *http.Server
	store storage.Store
	cfg   *config.Config
	log   *slog.Logger
}

// New builds the HTTP server with all routes wired.
func New(cfg *config.Config, store storage.Store, log *slog.Logger) (*Server, error) {
	s := &Server{store: store, cfg: cfg, log: log}

	mux := http.NewServeMux()

	// JSON API (Go 1.22 method+pattern routing).
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/search", s.auth(s.handleSearch))
	mux.HandleFunc("GET /api/inboxes", s.auth(s.handleListInboxes))
	mux.HandleFunc("GET /api/inboxes/{inbox}/messages", s.auth(s.handleListMessages))
	mux.HandleFunc("DELETE /api/inboxes/{inbox}", s.auth(s.handleDeleteInbox))
	mux.HandleFunc("GET /api/messages/{id}", s.auth(s.handleGetMessage))
	mux.HandleFunc("GET /api/messages/{id}/raw", s.auth(s.handleGetRaw))
	mux.HandleFunc("GET /api/messages/{id}/attachments/{aid}", s.auth(s.handleGetAttachment))
	mux.HandleFunc("DELETE /api/messages/{id}", s.auth(s.handleDeleteMessage))

	// Bundled web UI (Nuxt SPA): everything not under /api/ is served from the
	// embedded FS, with a fallback to index.html for client-side routes.
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("/", spaHandler(sub))

	s.srv = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           logRequests(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s, nil
}

// Handler returns the root HTTP handler (API + UI). Useful for httptest.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// spaHandler serves static files from the embedded SPA bundle. Requests for
// paths that don't map to a real file fall back to index.html, so client-side
// routes (and a hard refresh on one) resolve correctly.
func spaHandler(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServerFS(fsys)
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if f, err := fsys.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Unknown path: hand the SPA entry point to the client router.
		clone := r.Clone(r.Context())
		clone.URL.Path = "/"
		fileServer.ServeHTTP(w, clone)
	}
}

// ListenAndServe blocks serving HTTP.
func (s *Server) ListenAndServe() error {
	s.log.Info("http listening", "addr", s.srv.Addr, "auth", s.cfg.APIToken != "")
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// auth wraps a handler with optional bearer-token enforcement. When no token is
// configured, the API is open (YOPmail-style); when one is set, requests must
// present it via `Authorization: Bearer <token>` or `?token=`.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIToken == "" {
			next(w, r)
			return
		}
		got := r.URL.Query().Get("token")
		if got == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				got = strings.TrimPrefix(h, "Bearer ")
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.APIToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid API token")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
}

// handleConfig exposes non-secret server config the UI needs (the catch-all
// domain to generate disposable addresses, and whether auth is required).
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	domain := ""
	if len(s.cfg.AllowedDomains) > 0 {
		domain = s.cfg.AllowedDomains[0]
	} else if s.cfg.Domain != "" {
		domain = s.cfg.Domain
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         Version,
		"domain":          domain,
		"allowed_domains": s.cfg.AllowedDomains,
		"auth_required":   s.cfg.APIToken != "",
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := atoiDefault(r.URL.Query().Get("limit"), 100)
	msgs, err := s.store.SearchMessages(r.Context(), q, limit)
	if err != nil {
		s.serverError(w, "search", err)
		return
	}
	writeJSON(w, http.StatusOK, metaList(msgs))
}

func (s *Server) handleListInboxes(w http.ResponseWriter, r *http.Request) {
	inboxes, err := s.store.ListInboxes(r.Context())
	if err != nil {
		s.serverError(w, "list inboxes", err)
		return
	}
	if inboxes == nil {
		inboxes = []model.InboxSummary{}
	}
	writeJSON(w, http.StatusOK, inboxes)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	inbox := normalize(r.PathValue("inbox"))
	limit := atoiDefault(r.URL.Query().Get("limit"), 100)
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)

	msgs, err := s.store.ListMessages(r.Context(), inbox, limit, offset)
	if err != nil {
		s.serverError(w, "list messages", err)
		return
	}
	writeJSON(w, http.StatusOK, metaList(msgs))
}

// metaList marshals a slim view so listing/search never ship bodies or raw.
func metaList(msgs []*model.Message) []any {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{
			"id":          m.ID,
			"inbox":       m.Inbox,
			"from":        m.From,
			"env_from":    m.EnvFrom,
			"to":          m.To,
			"subject":     m.Subject,
			"date":        m.Date,
			"received_at": m.ReceivedAt,
			"size":        m.Size,
		})
	}
	return out
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMessage(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if err != nil {
		s.serverError(w, "get message", err)
		return
	}
	// Enrich with server-computed signals so every consumer sees the same
	// extracted codes/links/unsubscribe and spam assessment.
	ex := extract.From(m.Headers, m.Text, m.HTML)
	sp := extract.Score(m.Headers, m.Subject, m.Text, m.HTML, len(ex.Links))
	writeJSON(w, http.StatusOK, struct {
		*model.Message
		Extracted extract.Result `json:"extracted"`
		Spam      extract.Spam   `json:"spam"`
	}{Message: m, Extracted: ex, Spam: sp})
}

func (s *Server) handleGetRaw(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMessage(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if err != nil {
		s.serverError(w, "get raw", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(m.Raw)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMessage(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if err != nil {
		s.serverError(w, "get attachment", err)
		return
	}
	aid := r.PathValue("aid")
	for i := range m.Attachments {
		a := &m.Attachments[i]
		if a.ID == aid {
			ct := a.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(a.Filename)+"\"")
			_, _ = w.Write(a.Content)
			return
		}
	}
	writeError(w, http.StatusNotFound, "attachment not found")
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteMessage(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	if err != nil {
		s.serverError(w, "delete message", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteInbox(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteInbox(r.Context(), normalize(r.PathValue("inbox")))
	if err != nil {
		s.serverError(w, "delete inbox", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// --- helpers ---

func (s *Server) serverError(w http.ResponseWriter, op string, err error) {
	s.log.Error(op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func sanitizeFilename(name string) string {
	name = strings.NewReplacer("\"", "", "\n", "", "\r", "", "/", "_", "\\", "_").Replace(name)
	if name == "" {
		return "attachment"
	}
	return name
}

// logRequests is a tiny access-log middleware.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start).String())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
