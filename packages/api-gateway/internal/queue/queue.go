// Package queue implements the Phase 7 approval-queue routes:
//   GET  /me/queue
//   POST /me/queue/{id}/approve
//   POST /me/queue/{id}/reject
//
// A draft.v1 document is "pending" when the PA creates it, "sent" once
// the user approves and Graph accepts sendMail, "rejected" when the
// user declines. The ACL already restricts the list to drafts the user
// has reader/writer on — no extra scoping needed here.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/documents"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/graph"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

type Handler struct {
	pool         *pgxpool.Pool
	documents    *documents.Store
	fga          *acl.Client
	graphStore   *graph.Store
	graphClient  *graph.Client
	simulateSend bool
}

type Options struct {
	GraphStore   *graph.Store
	GraphClient  *graph.Client
	SimulateSend bool
}

func NewHandler(pool *pgxpool.Pool, docs *documents.Store, fga *acl.Client, opts Options) *Handler {
	return &Handler{
		pool: pool, documents: docs, fga: fga,
		graphStore:   opts.GraphStore,
		graphClient:  opts.GraphClient,
		simulateSend: opts.SimulateSend,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /me/queue", auth.Require(http.HandlerFunc(h.list)))
	mux.Handle("POST /me/queue/{id}/approve", auth.Require(http.HandlerFunc(h.approve)))
	mux.Handle("POST /me/queue/{id}/reject", auth.Require(http.HandlerFunc(h.reject)))
}

// --- GET /me/queue --------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.FromContext(r.Context())

	var visible []uuid.UUID
	if h.fga != nil {
		objs, err := h.fga.ListObjects(r.Context(), principal.ID, acl.RelReader, acl.TypeDocument)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "acl_list_failed", err.Error(), nil)
			return
		}
		for _, o := range objs {
			id, err := uuid.Parse(strings.TrimPrefix(o, "document:"))
			if err == nil {
				visible = append(visible, id)
			}
		}
		if len(visible) == 0 {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}
	}

	const q = `
        SELECT id, schema_id, tenant_id, body, created_at, created_by, updated_at, updated_by, deleted_at
        FROM documents
        WHERE schema_id = 'draft.v1'
          AND deleted_at IS NULL
          AND ($1::uuid[] IS NULL OR id = ANY($1))
          AND body->>'status' = 'pending'
        ORDER BY created_at DESC
        LIMIT 200
    `
	rows, err := h.pool.Query(r.Context(), q, visible)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	defer rows.Close()

	items := make([]*documents.Document, 0)
	for rows.Next() {
		d := &documents.Document{}
		if err := rows.Scan(&d.ID, &d.SchemaID, &d.TenantID, &d.Body,
			&d.CreatedAt, &d.CreatedBy, &d.UpdatedAt, &d.UpdatedBy, &d.DeletedAt); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
		items = append(items, d)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- POST /me/queue/{id}/approve + /reject --------------------------------

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, true)
}
func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, false)
}

type draftBody struct {
	InReplyToDocumentID string   `json:"inReplyToDocumentId"`
	To                  []string `json:"to"`
	Cc                  []string `json:"cc,omitempty"`
	Subject             string   `json:"subject"`
	Body                string   `json:"body"`
	GeneratedByAgent    string   `json:"generatedByAgent"`
	GeneratedAt         string   `json:"generatedAt"`
	Status              string   `json:"status"`
	SentMessageID       string   `json:"sentMessageId,omitempty"`
}

func (h *Handler) transition(w http.ResponseWriter, r *http.Request, approve bool) {
	principal, _ := auth.FromContext(r.Context())

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", "id must be a uuid", nil)
		return
	}

	// ACL: principal must have writer on the draft (triage grants it).
	if h.fga != nil {
		allowed, err := h.fga.Check(r.Context(), principal.ID, acl.RelWriter, "document:"+id.String())
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "acl_check_failed", err.Error(), nil)
			return
		}
		if !allowed {
			httpx.WriteError(w, http.StatusForbidden, "forbidden",
				"principal is not a writer on this draft", nil)
			return
		}
	}

	doc, err := h.documents.Get(r.Context(), id)
	if errors.Is(err, documents.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "draft not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	if doc.SchemaID != "draft.v1" {
		httpx.WriteError(w, http.StatusBadRequest, "wrong_schema",
			"only draft.v1 documents are in the queue", nil)
		return
	}
	var body draftBody
	if err := json.Unmarshal(doc.Body, &body); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	if body.Status != "pending" {
		httpx.WriteError(w, http.StatusConflict, "not_pending",
			"draft is already "+body.Status, nil)
		return
	}

	if !approve {
		body.Status = "rejected"
		h.writeAndRespond(w, r.Context(), doc.ID, body, principal.ID)
		return
	}

	// --- approve: send the mail, then flip status to sent ---
	if err := h.sendOnBehalf(r.Context(), principal.ID, &body); err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "send_failed", err.Error(), nil)
		return
	}
	body.Status = "sent"
	h.writeAndRespond(w, r.Context(), doc.ID, body, principal.ID)
}

// sendOnBehalf either calls Graph /me/sendMail with the user's token or,
// when SimulateSend is true, records a deterministic fake sentMessageId.
func (h *Handler) sendOnBehalf(ctx context.Context, principalID string, body *draftBody) error {
	if h.simulateSend {
		body.SentMessageID = "sim-" + time.Now().UTC().Format("20060102T150405.000000000")
		slog.Info("simulate-send", "principal", principalID, "to", body.To, "subject", body.Subject)
		return nil
	}
	if h.graphClient == nil || h.graphStore == nil {
		return errors.New("graph connector not configured")
	}
	userID, err := uuid.Parse(strings.TrimPrefix(principalID, "user:"))
	if err != nil {
		return errors.New("approve must be called by a user principal")
	}
	account, err := h.graphStore.GetForUser(ctx, userID)
	if err != nil {
		return err
	}
	// Upsert is the cheapest path to a decrypted refresh token; the
	// poller's own path decrypts via ListActive but we can just refresh.
	tok, err := h.graphClient.Refresh(ctx, account.RefreshToken)
	if err != nil {
		return err
	}
	if err := h.graphClient.SendMail(ctx, tok.AccessToken, body.Subject, body.Body, body.To, body.Cc); err != nil {
		return err
	}
	body.SentMessageID = "graph-" + time.Now().UTC().Format("20060102T150405.000000000")
	return nil
}

func (h *Handler) writeAndRespond(w http.ResponseWriter, ctx context.Context, id uuid.UUID, body draftBody, principalID string) {
	raw, err := json.Marshal(body)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	updated, err := h.documents.UpdateBody(ctx, id, raw, principalID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}
