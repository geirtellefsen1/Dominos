package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// MeHandler returns the authenticated user's profile.
func MeHandler(users *UserStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := FromContext(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		id, err := uuid.Parse(strings.TrimPrefix(p.ID, "user:"))
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "bad principal id", nil)
			return
		}
		u, err := users.GetByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "not_found", "user not found", nil)
				return
			}
			httpx.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"principal": p,
			"user":      u,
		})
	})
}
