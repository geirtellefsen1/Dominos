// Package cors is a minimal CORS middleware for the admin UI. Only
// enabled when DOMINION_CORS_ORIGIN is set (e.g. http://localhost:3100
// during `make admin-ui` development). Multiple origins can be listed
// comma-separated.
package cors

import (
	"net/http"
	"strings"
)

// Middleware returns an http.Handler wrapper. Behaviour:
//
//   - Origin in the allowlist         → reflect the Origin back, set
//                                        credentials + methods + headers.
//   - OPTIONS preflight               → 204 with the reflected headers.
//   - Origin missing or not allowed   → pass through unchanged; the
//                                        browser will drop the response.
//
// When allowedOrigins is empty the returned middleware is a no-op so
// callers can chain it unconditionally.
func Middleware(allowedOrigins string) func(http.Handler) http.Handler {
	allowed := parse(allowedOrigins)
	return func(next http.Handler) http.Handler {
		if len(allowed) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && isAllowed(origin, allowed) {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers",
					"Authorization, Content-Type, X-Dominion-Dev-Principal")
				h.Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parse(raw string) []string {
	if raw == "" {
		return nil
	}
	out := make([]string, 0)
	for _, o := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(o); s != "" {
			out = append(out, strings.TrimRight(s, "/"))
		}
	}
	return out
}

func isAllowed(origin string, allowed []string) bool {
	origin = strings.TrimRight(origin, "/")
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}
