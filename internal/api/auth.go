package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nees/zorail/internal/auth"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// principal is the authenticated identity behind a request.
type principal struct {
	admin  bool          // legacy global token, or a key with the admin scope
	userID string        // owner of the key ("" for legacy admin / open mode)
	key    *model.APIKey // nil for legacy admin / open mode
}

// has reports whether the principal carries scope s.
func (p *principal) has(s model.Scope) bool {
	if p.admin {
		return true
	}
	return p.key != nil && p.key.Has(s)
}

// allows reports whether the principal may act on the given normalized inbox,
// honoring the key's inbox-prefix scope. Admin/open principals allow all.
func (p *principal) allows(inbox string) bool {
	if p.admin || p.key == nil {
		return true
	}
	return p.key.Allows(inbox)
}

type ctxKey int

const principalKey ctxKey = 0

func principalFrom(ctx context.Context) *principal {
	if p, ok := ctx.Value(principalKey).(*principal); ok {
		return p
	}
	return &principal{} // unauthenticated, no scopes
}

var errNoCreds = errors.New("no credentials")

// bearer extracts a token from the Authorization header or ?token= query.
func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// resolve maps a request's credentials to a principal. Returns errNoCreds when
// none were presented so callers can apply legacy-open behavior.
func (s *Server) resolve(r *http.Request) (*principal, error) {
	tok := bearer(r)
	if tok == "" {
		return nil, errNoCreds
	}
	// Legacy global token → admin.
	if s.cfg.APIToken != "" && auth.ConstantEqual(tok, s.cfg.APIToken) {
		return &principal{admin: true}, nil
	}
	if auth.LooksLikeKey(tok) {
		k, err := s.store.GetAPIKeyByHash(r.Context(), auth.HashKey(tok))
		if errors.Is(err, storage.ErrNotFound) {
			return nil, errors.New("invalid API key")
		}
		if err != nil {
			return nil, err
		}
		return &principal{admin: k.Has(model.ScopeAdmin), userID: k.UserID, key: k}, nil
	}
	return nil, errors.New("invalid credentials")
}

// authn requires an authenticated principal carrying at least scope min. When
// no global token is configured AND no credentials are presented, the request
// runs as an implicit admin (preserves Zorail's open single-user mode).
func (s *Server) authn(min model.Scope, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.resolve(r)
		if errors.Is(err, errNoCreds) {
			if s.cfg.APIToken == "" {
				p = &principal{admin: true} // open mode
			} else {
				writeError(w, http.StatusUnauthorized, "missing API token")
				return
			}
		} else if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		if !p.has(min) {
			writeError(w, http.StatusForbidden, "insufficient scope: need "+string(min))
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	}
}

// authRead guards message-reading routes. It preserves the historical behavior:
// open when no global token is set; otherwise a valid token/key with read scope
// is required. The resolved principal is attached so handlers can enforce
// inbox-prefix scope.
func (s *Server) authRead(next http.HandlerFunc) http.HandlerFunc {
	return s.authn(model.ScopeRead, next)
}
