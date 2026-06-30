package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nees/zorail/internal/id"
	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// pickDomain returns the domain Zorail mints/reserves addresses under.
func (s *Server) pickDomain() string {
	if len(s.cfg.AllowedDomains) > 0 {
		return s.cfg.AllowedDomains[0]
	}
	return s.cfg.Domain
}

func (s *Server) handleListAddresses(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "address management requires a user account")
		return
	}
	addrs, err := s.store.ListAddresses(r.Context(), p.userID)
	if err != nil {
		s.serverError(w, "list addresses", err)
		return
	}
	if addrs == nil {
		addrs = []*model.Address{}
	}
	writeJSON(w, http.StatusOK, addrs)
}

type reserveReq struct {
	Address   string   `json:"address"`    // full address; or
	Prefix    string   `json:"prefix"`     // local-part prefix to mint <prefix>-<rand>@domain
	Type      string   `json:"type"`       // reserved | forward
	ForwardTo []string `json:"forward_to"` // forward destinations (forward type)
}

func (s *Server) handleReserveAddress(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "address management requires a user account")
		return
	}
	var req reserveReq
	if !decodeJSON(w, r, &req) {
		return
	}

	typ := model.AddressType(req.Type)
	if typ != model.AddrReserved && typ != model.AddrForward {
		writeError(w, http.StatusBadRequest, "type must be 'reserved' or 'forward'")
		return
	}

	address := normalize(req.Address)
	if address == "" {
		local := strings.ToLower(strings.TrimSpace(req.Prefix))
		if local == "" {
			local = "u"
		}
		address = fmt.Sprintf("%s-%s@%s", local, id.New()[:10], s.pickDomain())
	}
	if !strings.Contains(address, "@") {
		writeError(w, http.StatusBadRequest, "address must contain @")
		return
	}
	if !s.cfg.AllowsRecipient(address) {
		writeError(w, http.StatusBadRequest, "address domain is not served by this instance")
		return
	}
	if !p.allows(address) {
		writeError(w, http.StatusForbidden, "address is outside this key's inbox-prefix scope")
		return
	}

	// Reject claiming an address owned by someone else.
	if existing, err := s.store.GetAddress(r.Context(), address); err == nil && existing.OwnerUserID != p.userID {
		writeError(w, http.StatusConflict, "address already reserved")
		return
	} else if err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.serverError(w, "get address", err)
		return
	}

	a := &model.Address{
		Address:        address,
		Type:           typ,
		OwnerUserID:    p.userID,
		ForwardTo:      normalizeDests(req.ForwardTo),
		ForwardEnabled: typ == model.AddrForward && len(req.ForwardTo) > 0,
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.store.UpsertAddress(r.Context(), a); err != nil {
		s.serverError(w, "reserve address", err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

type updateAddrReq struct {
	ForwardTo      *[]string `json:"forward_to"`
	ForwardEnabled *bool     `json:"forward_enabled"`
}

func (s *Server) handleUpdateAddress(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	address := normalize(r.PathValue("address"))
	a, err := s.store.GetAddress(r.Context(), address)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "address not reserved")
		return
	}
	if err != nil {
		s.serverError(w, "get address", err)
		return
	}
	if !p.admin && a.OwnerUserID != p.userID {
		writeError(w, http.StatusForbidden, "not your address")
		return
	}
	var req updateAddrReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ForwardTo != nil {
		a.ForwardTo = normalizeDests(*req.ForwardTo)
		a.Type = model.AddrForward
	}
	if req.ForwardEnabled != nil {
		a.ForwardEnabled = *req.ForwardEnabled
	}
	if err := s.store.UpsertAddress(r.Context(), a); err != nil {
		s.serverError(w, "update address", err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleReleaseAddress(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "address management requires a user account")
		return
	}
	err := s.store.DeleteAddress(r.Context(), normalize(r.PathValue("address")), p.userID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "address not found")
		return
	}
	if err != nil {
		s.serverError(w, "release address", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Mailbox verification (forwarding destinations) ---

type verifyReq struct {
	Dest string `json:"dest"`
}

func (s *Server) handleVerifyRequest(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.userID == "" {
		writeError(w, http.StatusBadRequest, "verification requires a user account")
		return
	}
	var req verifyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	dest := normalize(req.Dest)
	if !strings.Contains(dest, "@") {
		writeError(w, http.StatusBadRequest, "valid destination address required")
		return
	}
	token := id.New()
	v := &model.MailboxVerification{Dest: dest, UserID: p.userID, Token: token, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateVerification(r.Context(), v); err != nil {
		s.serverError(w, "create verification", err)
		return
	}

	confirmURL := fmt.Sprintf("%s/api/verify/confirm?token=%s", s.baseURL(r), token)
	resp := map[string]any{"dest": dest, "status": "pending"}

	if s.deps.Mailer != nil {
		raw := verificationEmail(s.pickDomain(), dest, confirmURL)
		if err := s.deps.Mailer.Send(r.Context(), "verify@"+s.pickDomain(), []string{dest}, raw); err != nil {
			s.log.Error("send verification mail", "err", err, "dest", dest)
			writeError(w, http.StatusBadGateway, "could not send verification email")
			return
		}
		resp["sent"] = true
	} else {
		// No relay configured: hand back the link so the operator can confirm.
		resp["confirm_url"] = confirmURL
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) handleVerifyConfirm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "token required")
		return
	}
	v, err := s.store.GetVerificationByToken(r.Context(), token)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invalid or expired token")
		return
	}
	if err != nil {
		s.serverError(w, "get verification", err)
		return
	}
	if err := s.store.MarkVerified(r.Context(), v.Dest, time.Now().UTC()); err != nil {
		s.serverError(w, "mark verified", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dest": v.Dest, "status": "verified"})
}

// baseURL reconstructs the externally-visible base URL for building links.
func (s *Server) baseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") == "" {
		scheme = "http"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func verificationEmail(domain, dest, url string) []byte {
	body := fmt.Sprintf("From: Zorail <verify@%s>\r\n"+
		"To: %s\r\n"+
		"Subject: Confirm forwarding to this mailbox\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"Confirm that Zorail may forward mail to this address by opening:\r\n\r\n%s\r\n\r\n"+
		"If you did not request this, ignore this email.\r\n", domain, dest, url)
	return []byte(body)
}

func normalizeDests(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		if d = normalize(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}
