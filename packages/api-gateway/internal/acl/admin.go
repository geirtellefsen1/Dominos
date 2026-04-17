package acl

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// AdminHandler exposes POST /admin/acl/grant and /admin/acl/revoke.
// Bearer-token authenticated via DOMINION_ADMIN_TOKEN — the Phase 9 admin
// UI will replace this with session-role auth.
type AdminHandler struct {
	client *Client
	token  string
}

func NewAdminHandler(c *Client, token string) *AdminHandler {
	return &AdminHandler{client: c, token: token}
}

func (h *AdminHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /admin/acl/grant", h.auth(http.HandlerFunc(h.grant)))
	mux.Handle("POST /admin/acl/revoke", h.auth(http.HandlerFunc(h.revoke)))
}

func (h *AdminHandler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.token == "" {
			httpx.WriteError(w, http.StatusServiceUnavailable, "admin_disabled",
				"DOMINION_ADMIN_TOKEN not configured", nil)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized",
				"invalid admin bearer token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type grantRequest struct {
	Principal string `json:"principal"` // e.g. "user:<uuid>" or "agent:<uuid>"
	Relation  string `json:"relation"`  // "reader" | "writer" | "owner"
	Resource  string `json:"resource"`  // e.g. "document:<uuid>"
}

func (r grantRequest) validate() error {
	if r.Principal == "" || r.Relation == "" || r.Resource == "" {
		return errValidate("principal, relation, resource are required")
	}
	switch r.Relation {
	case RelOwner, RelReader, RelWriter:
	default:
		return errValidate("relation must be owner|reader|writer")
	}
	if !strings.Contains(r.Principal, ":") {
		return errValidate("principal must be <type>:<id>")
	}
	if !strings.HasPrefix(r.Resource, TypeDocument+":") {
		return errValidate("resource must be document:<uuid>")
	}
	return nil
}

type validateErr string

func (e validateErr) Error() string { return string(e) }
func errValidate(s string) error    { return validateErr(s) }

func (h *AdminHandler) grant(w http.ResponseWriter, r *http.Request) {
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := req.validate(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := h.client.Write(r.Context(), Tuple{
		User:     req.Principal,
		Relation: req.Relation,
		Object:   req.Resource,
	}); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "acl_write_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "granted"})
}

func (h *AdminHandler) revoke(w http.ResponseWriter, r *http.Request) {
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := req.validate(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := h.client.Delete(r.Context(), Tuple{
		User:     req.Principal,
		Relation: req.Relation,
		Object:   req.Resource,
	}); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "acl_delete_failed", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
