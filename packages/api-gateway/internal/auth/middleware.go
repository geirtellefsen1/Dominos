package auth

import (
	"log/slog"
	"net/http"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// Attach reads the session cookie (if present) and stores a Principal on
// the request context. It never rejects; routes needing auth wrap with
// Require.
func Attach(sessions *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookieName)
			if err == nil && c.Value != "" {
				if info, err := sessions.Verify(r.Context(), c.Value); err == nil {
					p := Principal{
						Kind:  "user",
						ID:    "user:" + info.UserID.String(),
						Email: info.Email,
					}
					r = r.WithContext(WithPrincipal(r.Context(), p))
				} else {
					slog.Debug("session verify failed", "err", err)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Require rejects the request with 401 unless a Principal is attached.
func Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
