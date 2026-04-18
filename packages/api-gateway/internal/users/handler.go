// Package users owns the admin-side user management routes (phase 8).
// Routine provisioning still flows through SCIM; this package adds the
// DELETE /admin/users/{id} route that performs the full one-revoke per
// spec §3.4: mark inactive, kill sessions, strip ACL tuples.
package users

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/scim"
)

// RevokeSubjectTuples strips every FGA tuple where the given principal
// is the subject. Wired in main.go to avoid pulling acl into this
// package (which would re-introduce the agents -> acl -> audit -> auth
// cycle).
type RevokeSubjectTuples func(ctx context.Context, principal string) (int, error)

type Handler struct {
	scim         *scim.Store
	sessions     *auth.SessionStore
	revokeTuples RevokeSubjectTuples
	token        string
}

func NewHandler(scimStore *scim.Store, sessions *auth.SessionStore, revokeTuples RevokeSubjectTuples, token string) *Handler {
	return &Handler{scim: scimStore, sessions: sessions, revokeTuples: revokeTuples, token: token}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("DELETE /admin/users/{id}", h.auth(http.HandlerFunc(h.revoke)))
}

func (h *Handler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.token == "" {
			httpx.WriteError(w, http.StatusServiceUnavailable, "admin_disabled",
				"DOMINION_ADMIN_TOKEN not configured", nil)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) != 1 {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid admin bearer token", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// revoke implements DELETE /admin/users/{id} — spec §3.4's one-revoke
// for humans. The three steps are performed in order:
//
//  1. mark the identity row inactive (SCIM Store.SetActive),
//  2. revoke every live session so the user can't keep using an
//     already-issued JWT,
//  3. strip every FGA tuple where user:<id> is a subject so the ACL
//     graph no longer authorises them for any document.
//
// On partial failure we return a detailed error so the admin can retry;
// step 1 is idempotent and steps 2–3 can be re-run by calling DELETE
// again.
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", "id must be a uuid", nil)
		return
	}

	// Step 1: deactivate.
	row, err := h.scim.SetActive(r.Context(), id, false)
	if errors.Is(err, scim.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "user not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	// Step 2: revoke sessions.
	sessionsRevoked, err := h.sessions.RevokeAllForUser(r.Context(), id)
	if err != nil {
		slog.Error("revoke user sessions", "user", id, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "session_revoke_failed", err.Error(), nil)
		return
	}

	// Step 3: strip FGA tuples.
	tuplesRemoved := 0
	if h.revokeTuples != nil {
		n, err := h.revokeTuples(r.Context(), "user:"+id.String())
		if err != nil {
			slog.Error("revoke user fga tuples", "user", id, "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "acl_cleanup_failed",
				"identity marked revoked and sessions killed, but ACL cleanup failed; retry the DELETE", nil)
			return
		}
		tuplesRemoved = n
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":              row,
		"sessions_revoked":  sessionsRevoked,
		"tuples_removed":    tuplesRemoved,
	})
}
