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

	// --- reject: single atomic pending -> rejected, no Graph call ---
	if !approve {
		updated, err := h.documents.UpdateDraftStatus(r.Context(), id,
			"pending", "rejected", nil, principal.ID)
		if errors.Is(err, documents.ErrStatusMismatch) {
			httpx.WriteError(w, http.StatusConflict, "not_pending",
				"draft is not pending — already sent, rejected, or in flight", nil)
			return
		}
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, updated)
		return
	}

	// --- approve: idempotent state machine ---
	//
	//   pending -> sending   (CAS; wins the right to call Graph)
	//   sending -> sent      (on Graph success, with real internetMessageId)
	//   sending -> pending   (on Graph error, so the user can retry)
	//
	// A second concurrent approve sees status=sending and gets 409,
	// which is what we want: no double-send.

	acquired, err := h.documents.UpdateDraftStatus(r.Context(), id,
		"pending", "sending", nil, principal.ID)
	if errors.Is(err, documents.ErrStatusMismatch) {
		httpx.WriteError(w, http.StatusConflict, "not_pending",
			"draft is not pending — already sent, rejected, or in flight", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	if acquired.SchemaID != "draft.v1" {
		// Release: flip back to pending (shouldn't happen — UpdateDraftStatus filters on schema_id).
		_, _ = h.documents.UpdateDraftStatus(r.Context(), id, "sending", "pending", nil, principal.ID)
		httpx.WriteError(w, http.StatusBadRequest, "wrong_schema",
			"only draft.v1 documents are in the queue", nil)
		return
	}
	var body draftBody
	if err := json.Unmarshal(acquired.Body, &body); err != nil {
		_, _ = h.documents.UpdateDraftStatus(r.Context(), id, "sending", "pending", nil, principal.ID)
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	internetMessageID, err := h.sendOnBehalf(r.Context(), principal.ID, &body)
	if err != nil {
		// Release the sending lock so the user can retry.
		_, _ = h.documents.UpdateDraftStatus(r.Context(), id, "sending", "pending", nil, principal.ID)
		httpx.WriteError(w, http.StatusBadGateway, "send_failed", err.Error(), nil)
		return
	}

	// Commit: sending -> sent, persisting the real Graph
	// internetMessageId so downstream dedup can use it.
	sent, err := h.documents.UpdateDraftStatus(r.Context(), id,
		"sending", "sent",
		map[string]string{"sentMessageId": internetMessageID},
		principal.ID)
	if err != nil {
		// Email is out but DB commit failed — operator intervention
		// needed. Loud log, 500 response with the real message id so
		// the operator can reconcile.
		slog.Error("approve commit failed after Graph send",
			"doc", id, "internetMessageID", internetMessageID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "commit_failed",
			"email sent but draft record stuck in 'sending' — operator must reconcile. Sent message id: "+internetMessageID,
			nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sent)
}

// sendOnBehalf either calls Graph (create-draft + send, returning the
// real RFC 5322 internetMessageId) or, when SimulateSend is true,
// fabricates a stable dedup key derived from the draft body so the
// smoke test has something deterministic to assert on.
func (h *Handler) sendOnBehalf(ctx context.Context, principalID string, body *draftBody) (string, error) {
	if h.simulateSend {
		fake := "sim:" + time.Now().UTC().Format("20060102T150405.000000000")
		slog.Info("simulate-send", "principal", principalID, "to", body.To, "subject", body.Subject)
		return fake, nil
	}
	if h.graphClient == nil || h.graphStore == nil {
		return "", errors.New("graph connector not configured")
	}
	userID, err := uuid.Parse(strings.TrimPrefix(principalID, "user:"))
	if err != nil {
		return "", errors.New("approve must be called by a user principal")
	}
	// GetForUserWithToken decrypts refresh_token_ciphertext (Sprint 1 #1
	// fix — pre-Sprint-1 we called GetForUser and passed the empty
	// string to Graph.Refresh).
	account, err := h.graphStore.GetForUserWithToken(ctx, userID)
	if err != nil {
		return "", err
	}
	tok, err := h.graphClient.Refresh(ctx, account.RefreshToken)
	if err != nil {
		return "", err
	}
	internetID, err := h.graphClient.SendMail(ctx, tok.AccessToken, body.Subject, body.Body, body.To, body.Cc)
	if err != nil {
		return "", err
	}
	return internetID, nil
}
