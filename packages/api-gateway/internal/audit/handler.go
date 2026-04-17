package audit

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// Handler exposes /admin/audit (export) and /admin/audit/key (pubkey).
// Both routes require DOMINION_ADMIN_TOKEN via Authorization: Bearer.
type Handler struct {
	store *Store
	token string
}

func NewHandler(store *Store, token string) *Handler {
	return &Handler{store: store, token: token}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /admin/audit", h.auth(http.HandlerFunc(h.export)))
	// The key is public by design — the compliance officer needs it to
	// verify exports — but we still gate it behind the admin token for
	// consistency; in production it can be served unauthenticated.
	mux.Handle("GET /admin/audit/key", h.auth(http.HandlerFunc(h.key)))
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

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := QueryParams{Actor: q.Get("actor")}
	if s := q.Get("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_from", err.Error(), nil)
			return
		}
		params.From = t
	}
	if s := q.Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_to", err.Error(), nil)
			return
		}
		params.To = t
	}

	events, err := h.store.Query(r.Context(), params)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}
	if events == nil {
		events = []*Event{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"public_key_base64": h.store.Keys().PublicKeyBase64(),
		"key_id":            h.store.Keys().KeyID,
		"events":            events,
		"count":             len(events),
	})
}

func (h *Handler) key(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"public_key_base64": h.store.Keys().PublicKeyBase64(),
		"key_id":            h.store.Keys().KeyID,
		"algorithm":         "Ed25519",
	})
}
