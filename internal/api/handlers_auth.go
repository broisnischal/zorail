package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nees/zorail/internal/auth"
	"github.com/nees/zorail/internal/id"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// instance setting keys.
const (
	settingOrgName           = "org_name"
	settingAdminUserID       = "admin_user_id"
	settingAllowRegistration = "allow_registration"
)

type credsReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSetupStatus reports whether the instance still needs first-run setup.
// Open (the onboarding screen calls it before any account exists).
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.serverError(w, "count users", err)
		return
	}
	org, _ := s.store.GetSetting(r.Context(), settingOrgName)
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup":  n == 0,
		"organization": org,
		"version":      Version,
	})
}

type setupReq struct {
	Organization string `json:"organization"`
	Email        string `json:"email"`
	Password     string `json:"password"`
}

// handleSetup creates the first administrator and names the organization. It is
// allowed exactly once — when no users exist yet (Coolify-style first boot).
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.serverError(w, "count users", err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "this instance is already configured")
		return
	}
	var req setupReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "valid email and password (min 8 chars) required")
		return
	}
	ph, err := auth.HashPassword(req.Password)
	if err != nil {
		s.serverError(w, "hash password", err)
		return
	}
	u := &model.User{ID: id.New(), Email: req.Email, PasswordHash: ph, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		s.serverError(w, "create admin", err)
		return
	}
	org := strings.TrimSpace(req.Organization)
	if org == "" {
		org = "Zorail"
	}
	_ = s.store.SetSetting(r.Context(), settingOrgName, org)
	_ = s.store.SetSetting(r.Context(), settingAdminUserID, u.ID)

	k, err := s.mintKey(r, u.ID, "admin", []model.Scope{model.ScopeAdmin}, "")
	if err != nil {
		s.serverError(w, "mint admin key", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": u, "token": k.Secret, "organization": org, "scopes": k.Scopes})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// First account is created through /api/setup; afterwards self-registration
	// is closed unless the admin has explicitly enabled it.
	if allow, _ := s.store.GetSetting(r.Context(), settingAllowRegistration); allow != "true" {
		writeError(w, http.StatusForbidden, "registration is closed; ask your administrator for access")
		return
	}
	var req credsReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "valid email and password (min 8 chars) required")
		return
	}
	ph, err := auth.HashPassword(req.Password)
	if err != nil {
		s.serverError(w, "hash password", err)
		return
	}
	u := &model.User{ID: id.New(), Email: req.Email, PasswordHash: ph, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateUser(r.Context(), u); errors.Is(err, storage.ErrConflict) {
		writeError(w, http.StatusConflict, "email already registered")
		return
	} else if err != nil {
		s.serverError(w, "create user", err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if errors.Is(err, storage.ErrNotFound) || (u != nil && !auth.CheckPassword(u.PasswordHash, req.Password)) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		s.serverError(w, "login", err)
		return
	}
	// The organization admin gets an admin-scoped session; everyone else manage.
	scopes := []model.Scope{model.ScopeManage}
	if adminID, _ := s.store.GetSetting(r.Context(), settingAdminUserID); adminID == u.ID {
		scopes = []model.Scope{model.ScopeAdmin}
	}
	k, err := s.mintKey(r, u.ID, "login", scopes, "")
	if err != nil {
		s.serverError(w, "mint key", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "token": k.Secret, "scopes": k.Scopes})
}

// mintKey creates and persists a key for userID and returns it with Secret set.
func (s *Server) mintKey(r *http.Request, userID, name string, scopes []model.Scope, prefix string) (*model.APIKey, error) {
	secret, hash := auth.NewKey()
	k := &model.APIKey{
		ID:          id.New(),
		UserID:      userID,
		Name:        name,
		KeyHash:     hash,
		Scopes:      scopes,
		InboxPrefix: prefix,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.CreateAPIKey(r.Context(), k); err != nil {
		return nil, err
	}
	k.Secret = secret
	return k, nil
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "key management requires a user account; register and log in")
		return
	}
	keys, err := s.store.ListAPIKeys(r.Context(), p.userID)
	if err != nil {
		s.serverError(w, "list keys", err)
		return
	}
	if keys == nil {
		keys = []*model.APIKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

type createKeyReq struct {
	Name        string   `json:"name"`
	Scopes      []string `json:"scopes"`
	InboxPrefix string   `json:"inbox_prefix"`
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "key management requires a user account; register and log in")
		return
	}
	var req createKeyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	scopes, err := parseScopes(req.Scopes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A non-admin principal cannot mint a key more powerful than itself.
	for _, sc := range scopes {
		if sc == model.ScopeAdmin && !p.admin {
			writeError(w, http.StatusForbidden, "cannot grant admin scope")
			return
		}
	}
	if req.Name == "" {
		req.Name = "key"
	}
	k, err := s.mintKey(r, p.userID, req.Name, scopes, strings.ToLower(strings.TrimSpace(req.InboxPrefix)))
	if err != nil {
		s.serverError(w, "create key", err)
		return
	}
	writeJSON(w, http.StatusCreated, k) // includes Secret exactly once
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "key management requires a user account")
		return
	}
	err := s.store.DeleteAPIKey(r.Context(), r.PathValue("id"), p.userID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	if err != nil {
		s.serverError(w, "delete key", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseScopes(in []string) ([]model.Scope, error) {
	if len(in) == 0 {
		return []model.Scope{model.ScopeRead}, nil
	}
	out := make([]model.Scope, 0, len(in))
	for _, s := range in {
		switch model.Scope(s) {
		case model.ScopeRead, model.ScopeManage, model.ScopeAdmin:
			out = append(out, model.Scope(s))
		default:
			return nil, errors.New("unknown scope: " + s)
		}
	}
	return out, nil
}

// decodeJSON reads a JSON body, writing a 400 and returning false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
