package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nees/zorail/internal/extract"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

type ingestReq struct {
	Raw     string   `json:"raw"`      // RFC 5322 source as a string
	EnvFrom string   `json:"env_from"` // envelope sender (optional)
	Rcpts   []string `json:"rcpts"`    // recipients to fan out to
}

// handleIngest accepts a message over HTTP — the path a Cloudflare Email Worker
// or a relay uses to push mail into Zorail without opening port 25.
//
// JSON mode: POST application/json {raw, env_from, rcpts[]}.
// Raw mode:  POST message/rfc822 with rcpts in ?rcpt= (repeatable) and
//
//	envelope sender in ?env_from=.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if s.deps.Ingest == nil {
		writeError(w, http.StatusServiceUnavailable, "ingest not configured")
		return
	}

	var raw []byte
	var envFrom string
	var rcpts []string

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req ingestReq
		if !decodeJSON(w, r, &req) {
			return
		}
		raw, envFrom, rcpts = []byte(req.Raw), req.EnvFrom, req.Rcpts
	} else {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxMessageBytes))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "message too large")
			return
		}
		raw = b
		envFrom = r.URL.Query().Get("env_from")
		rcpts = r.URL.Query()["rcpt"]
	}

	if len(raw) == 0 || len(rcpts) == 0 {
		writeError(w, http.StatusBadRequest, "raw body and at least one recipient required")
		return
	}

	n, err := s.deps.Ingest.Accept(r.Context(), raw, envFrom, rcpts)
	if err != nil {
		s.serverError(w, "ingest", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"stored": n})
}

// handleWait blocks until a message newer than ?after=<id> lands in the inbox,
// or the timeout elapses (204). It powers test suites and agents that want to
// "wait for the OTP" instead of busy-polling.
func (s *Server) handleWait(w http.ResponseWriter, r *http.Request) {
	inbox := normalize(r.PathValue("inbox"))
	if !principalFrom(r.Context()).allows(inbox) {
		writeError(w, http.StatusForbidden, "inbox outside key scope")
		return
	}
	after := r.URL.Query().Get("after")
	timeout := time.Duration(clampInt(atoiDefault(r.URL.Query().Get("timeout"), 25), 1, 120)) * time.Second

	// Subscribe before the first DB check so we never miss an arrival.
	var ch <-chan string
	if s.deps.Hub != nil {
		c, cancel := s.deps.Hub.Subscribe(inbox)
		defer cancel()
		ch = c
	}

	if m := s.newestAfter(r, inbox, after); m != nil {
		s.writeMessage(w, m)
		return
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	poll := time.NewTicker(2 * time.Second) // fallback in case a signal is missed
	defer poll.Stop()

	for {
		select {
		case <-ch:
			if m := s.newestAfter(r, inbox, after); m != nil {
				s.writeMessage(w, m)
				return
			}
		case <-poll.C:
			if m := s.newestAfter(r, inbox, after); m != nil {
				s.writeMessage(w, m)
				return
			}
		case <-deadline.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			return
		}
	}
}

// newestAfter returns the newest fully-populated message in inbox whose ID
// sorts after `after` (ULID-style IDs are time-ordered), or nil.
func (s *Server) newestAfter(r *http.Request, inbox, after string) *model.Message {
	msgs, err := s.store.ListMessages(r.Context(), inbox, 1, 0)
	if err != nil || len(msgs) == 0 {
		return nil
	}
	if after != "" && msgs[0].ID <= after {
		return nil
	}
	full, err := s.store.GetMessage(r.Context(), msgs[0].ID)
	if errors.Is(err, storage.ErrNotFound) || err != nil {
		return nil
	}
	return full
}

// writeMessage emits a message with the same enrichment as GET /api/messages/{id}.
func (s *Server) writeMessage(w http.ResponseWriter, m *model.Message) {
	ex := extract.From(m.Headers, m.Text, m.HTML)
	sp := extract.Score(m.Headers, m.Subject, m.Text, m.HTML, len(ex.Links))
	writeJSON(w, http.StatusOK, struct {
		*model.Message
		Extracted extract.Result `json:"extracted"`
		Spam      extract.Spam   `json:"spam"`
	}{Message: m, Extracted: ex, Spam: sp})
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
