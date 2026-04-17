package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/agents"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/ca"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// AttachOptions controls how Attach resolves a Principal.
type AttachOptions struct {
	// DevPrincipalHeader enables the X-Dominion-Dev-Principal shim.
	DevPrincipalHeader bool
	// Agents, when set, is consulted whenever the request presents a
	// client certificate. When nil, mTLS principals are not resolved
	// (phase 2-only deployments).
	Agents *agents.Store
}

// Attach attaches a Principal to the request context when one can be
// resolved. Priority:
//  1. valid session cookie (human, phase 2)
//  2. valid client cert signed by the Dominion CA (agent, phase 5)
//  3. dev principal header (opt-in shim)
// It never rejects; routes needing auth wrap with Require.
func Attach(sessions *SessionStore, opts AttachOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Session cookie.
			if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
				if info, err := sessions.Verify(r.Context(), c.Value); err == nil {
					next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), Principal{
						Kind:  "user",
						ID:    "user:" + info.UserID.String(),
						Email: info.Email,
					})))
					return
				} else {
					slog.Debug("session verify failed", "err", err)
				}
			}

			// 2. Client certificate (agent principal).
			if opts.Agents != nil && r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
				cert := r.TLS.PeerCertificates[0]
				tp := ca.Thumbprint(cert.Raw)
				agent, err := opts.Agents.GetByThumbprint(r.Context(), tp)
				if err == nil && agent.Active {
					next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), Principal{
						Kind: "agent",
						ID:   "agent:" + agent.ID.String(),
					})))
					return
				}
				if err == nil && !agent.Active {
					httpx.WriteError(w, http.StatusUnauthorized, "agent_revoked",
						"client certificate has been revoked", nil)
					return
				}
				slog.Debug("client cert not recognised", "thumbprint", tp, "err", err)
			}

			// 3. Dev-mode header (opt-in).
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

// parseDevPrincipal accepts `user:<uuid>` or `agent:<uuid>`.
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
