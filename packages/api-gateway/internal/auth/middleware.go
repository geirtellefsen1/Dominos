package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// AttachOptions controls how the Attach middleware resolves a Principal.
type AttachOptions struct {
	// DevPrincipalHeader enables the X-Dominion-Dev-Principal shim. When
	// true, a request with no session cookie but a header value of the
	// form `user:<uuid>` is accepted as that user. Must be false in prod.
	DevPrincipalHeader bool
}

// Attach reads the session cookie (if present) and stores a Principal on
// the request context. It never rejects; routes needing auth wrap with
// Require.
func Attach(sessions *SessionStore, opts AttachOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Session cookie.
			if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
				if info, err := sessions.Verify(r.Context(), c.Value); err == nil {
					p := Principal{
						Kind:  "user",
						ID:    "user:" + info.UserID.String(),
						Email: info.Email,
					}
					next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
					return
				} else {
					slog.Debug("session verify failed", "err", err)
				}
			}

			// 2. Dev-mode header (opt-in).
			if opts.DevPrincipalHeader {
				if hv := r.Header.Get("X-Dominion-Dev-Principal"); hv != "" {
					if p, ok := parseDevPrincipal(hv); ok {
						next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
						return
					}
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

// parseDevPrincipal accepts `user:<uuid>` or `agent:<uuid>` (phase 5).
// Anything else is rejected so the header can't be used to spoof admins.
func parseDevPrincipal(v string) (Principal, bool) {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return Principal{}, false
	}
	kind, id := parts[0], parts[1]
	if kind != "user" && kind != "agent" {
		return Principal{}, false
	}
	if _, err := uuid.Parse(id); err != nil {
		return Principal{}, false
	}
	return Principal{Kind: kind, ID: kind + ":" + id}, true
}
