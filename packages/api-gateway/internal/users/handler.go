// Package users owns the admin-side user management routes (phase 8).
// Routine provisioning still flows through SCIM; this package adds the
// DELETE /admin/users/{id} route that performs the full one-revoke per
// spec §3.4: mark inactive, kill sessions, strip ACL tuples, and — as
// of Sprint 2 #10 — cascade-revoke every agent the user owns.
package users

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/agents"
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
	agents       *agents.Store
	revokeTuples RevokeSubjectTuples
	token        string
}

func NewHandler(scimStore *scim.Store, sessions *auth.SessionStore, agentsStore *agents.Store, revokeTuples RevokeSubjectTuples, token string) *Handler {
	return &Handler{
		scim:         scimStore,
		sessions:     sessions,
		agents:       agentsStore,
		revokeTuples: revokeTuples,
		token:        token,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	admin := auth.RequireBearer(h.token, "root")
	mux.Handle("DELETE /admin/users/{id}", admin(http.HandlerFunc(h.revoke)))
}

type cascadeResult struct {
	AgentID       uuid.UUID `json:"agent_id"`
	TuplesRemoved int       `json:"tuples_removed"`
	Error         string    `json:"error,omitempty"`
}

// revoke implements DELETE /admin/users/{id} — spec §3.4's one-revoke
// for humans, extended in Sprint 2 #10 with cascade-to-owned-agents.
// Steps in order:
//
//  1. Mark the user row inactive (SCIM Store.SetActive).
//  2. Revoke every live session so the user can't keep using an
//     already-issued JWT.
//  3. Strip every FGA tuple where user:<id> is a subject.
//  4. Cascade: for every agent where owner_user_id = id and active,
//     (a) mark the agent row revoked, (b) strip its FGA tuples. A
//     best-effort per-agent loop — one agent failing doesn't block
//     the rest, and the response lists both successes and failures.
//
// On partial failure the response carries enough structure that a
// retry of DELETE resumes cleanly; steps 1–4 are all idempotent.
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", "id must be a uuid", nil)
		return
	}

	// Step 1: deactivate the user row.
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

	// Step 3: strip FGA tuples where user:<id> is a subject.
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

	// Step 4: cascade-revoke every agent the user owns.
	cascades := []cascadeResult{}
	if h.agents != nil {
		ownedIDs, err := h.agents.ListOwnedBy(r.Context(), id)
		if err != nil {
			slog.Error("list user agents", "user", id, "err", err)
			// Don't fail the whole request; the user is already revoked
			// and sessions / tuples are gone. Surface the cascade error
			// so the admin knows owned agents may still be active.
			cascades = append(cascades, cascadeResult{Error: "list owned agents: " + err.Error()})
		} else {
			for _, agentID := range ownedIDs {
				c := cascadeResult{AgentID: agentID}
				if _, err := h.agents.Revoke(r.Context(), agentID); err != nil {
					c.Error = "revoke row: " + err.Error()
					cascades = append(cascades, c)
					continue
				}
				if h.revokeTuples != nil {
					n, err := h.revokeTuples(r.Context(), "agent:"+agentID.String())
					if err != nil {
						c.Error = "acl cleanup: " + err.Error()
					}
					c.TuplesRemoved = n
				}
				cascades = append(cascades, c)
			}
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":              row,
		"sessions_revoked":  sessionsRevoked,
		"tuples_removed":    tuplesRemoved,
		"cascaded_agents":   cascades,
	})
}
