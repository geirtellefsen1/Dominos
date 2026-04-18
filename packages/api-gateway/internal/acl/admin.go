package acl

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/audit"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// AdminHandler exposes POST /admin/acl/grant and /admin/acl/revoke.
// Bearer-token authenticated via DOMINION_ADMIN_TOKEN — the Phase 9
// admin UI will replace this with session-role auth eventually.
//
// As of Sprint 1 #2, auth.RequireBearer attaches an `admin:root`
// synthetic principal on success so the audit tap logs who granted
// or revoked the tuple, instead of the old actor=anonymous.
type AdminHandler struct {
	client *Client
	token  string
}

func NewAdminHandler(c *Client, token string) *AdminHandler {
	return &AdminHandler{client: c, token: token}
}

func (h *AdminHandler) Register(mux *http.ServeMux) {
	admin := auth.RequireBearer(h.token, "root")
	mux.Handle("POST /admin/acl/grant", admin(http.HandlerFunc(h.grant)))
	mux.Handle("POST /admin/acl/revoke", admin(http.HandlerFunc(h.revoke)))
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
	audit.From(r.Context()).Action = "acl.grant"
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := req.validate(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	audit.From(r.Context()).Resource = req.Resource
	audit.From(r.Context()).OnBehalfOf = req.Principal
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
	audit.From(r.Context()).Action = "acl.revoke"
	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := req.validate(); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	audit.From(r.Context()).Resource = req.Resource
	audit.From(r.Context()).OnBehalfOf = req.Principal
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
