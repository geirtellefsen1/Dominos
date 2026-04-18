package agents

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/ca"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// RevokeSubjectTuples strips every FGA tuple where the given `<kind>:<id>`
// principal is the subject. Phase 8 wires this to revoke.SubjectTuples
// via a closure in main.go so the agents package stays off the acl ->
// audit -> auth -> agents import cycle.
type RevokeSubjectTuples func(ctx context.Context, principal string) (int, error)

// AdminAuth is an http middleware that enforces the admin bearer
// token and attaches an admin principal to the context. main.go wires
// this to auth.RequireBearer — the agents package can't import auth
// directly (auth already imports agents for AttachOptions.Agents,
// which would create a cycle) so we pass it in as a value.
type AdminAuth func(http.Handler) http.Handler

type Handler struct {
	store        *Store
	ca           *ca.CA
	revokeTuples RevokeSubjectTuples
	adminAuth    AdminAuth
}

func NewHandler(store *Store, ca *ca.CA, revokeTuples RevokeSubjectTuples, adminAuth AdminAuth) *Handler {
	return &Handler{store: store, ca: ca, revokeTuples: revokeTuples, adminAuth: adminAuth}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("POST /admin/agents", h.adminAuth(http.HandlerFunc(h.create)))
	mux.Handle("DELETE /admin/agents/{id}", h.adminAuth(http.HandlerFunc(h.revoke)))
	// The CA cert is public by design — anyone needs it to verify the
	// gateway's TLS cert and agent certs. Unauthenticated.
	mux.HandleFunc("GET /admin/ca/cert", h.caCert)
}

type createRequest struct {
	DisplayName string     `json:"display_name"`
	OwnerUserID *uuid.UUID `json:"owner_user_id,omitempty"`
}

type createResponse struct {
	Agent          *Agent `json:"agent"`
	CertPEM        string `json:"cert_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CACertPEM      string `json:"ca_cert_pem"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	// Audit action/resource left at defaults — the audit middleware
	// still records every call; importing the audit package here would
	// create a cycle (audit→auth→agents→audit).
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "display_name is required", nil)
		return
	}

	id := uuid.New()
	issued, err := h.ca.IssueAgent(id, req.DisplayName)
	if err != nil {
		slog.Error("issue agent cert", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "ca_issue_failed", err.Error(), nil)
		return
	}

	stored, err := h.store.Insert(r.Context(), &Agent{
		ID:          id,
		DisplayName: req.DisplayName,
		Thumbprint:  issued.Thumbprint,
		OwnerUserID: req.OwnerUserID,
	}, string(issued.CertPEM))
	if err != nil {
		slog.Error("insert agent", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, createResponse{
		Agent:         stored,
		CertPEM:       string(issued.CertPEM),
		PrivateKeyPEM: string(issued.KeyPEM),
		CACertPEM:     string(h.ca.CertPEM),
	})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", "id must be a uuid", nil)
		return
	}

	a, err := h.store.Revoke(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "agent not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	// Phase 8: strip every FGA tuple where this agent is a subject so
	// the next authenticated call landing here can't re-authorise on
	// any document the agent was previously allowed to read/write.
	removed := 0
	if h.revokeTuples != nil {
		n, err := h.revokeTuples(r.Context(), "agent:"+a.ID.String())
		if err != nil {
			slog.Error("revoke agent fga tuples", "agent", a.ID, "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "acl_cleanup_failed",
				"identity marked revoked but acl cleanup failed; retry the DELETE", nil)
			return
		}
		removed = n
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"agent":           a,
		"tuples_removed":  removed,
	})
}

func (h *Handler) caCert(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.ca.CertPEM)
}
