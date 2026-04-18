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
	// DevPrincipalHeader enables the X-Dominion-Dev-Principal shim. When
	// true, a request with no session cookie but a header value of the
	// form `user:<uuid>` or `agent:<uuid>` is resolved against the
	// respective store and accepted only if the identity is active.
	DevPrincipalHeader bool
	// Agents is the store used to:
	//   - resolve client certs to agent principals (mTLS path),
	//   - validate dev-mode `agent:<uuid>` headers.
	Agents *agents.Store
	// Users is the store used to validate dev-mode `user:<uuid>`
	// headers (so Phase 8 revocation propagates even when tests use
	// the dev shim instead of a real session).
	Users *UserStore
}

// Attach attaches a Principal to the request context when one can be
// resolved. Priority:
//  1. valid session cookie (user, phase 2)
//  2. valid client cert signed by the Dominion CA (agent, phase 5)
//  3. dev principal header (opt-in, active-checked)
// It never rejects on its own; routes needing auth wrap with Require.
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

			// 3. Dev-mode header (opt-in). Revoked identities are rejected
			//    here so phase 8 revocation tests work even when the
			//    smoke tests use the dev shim in place of real OIDC / mTLS.
			if opts.DevPrincipalHeader {
				if hv := r.Header.Get("X-Dominion-Dev-Principal"); hv != "" {
					p, status, ok := resolveDevPrincipal(r, hv, opts)
					if ok {
						next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
						return
					}
					if status != 0 {
						httpx.WriteError(w, status, "principal_revoked",
							"dev principal is inactive or unknown", nil)
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

// resolveDevPrincipal parses the header value and consults the relevant
// store to verify the identity is active. Returns:
//   - (principal, 0, true) on success,
//   - (_, status, false) when the identity is known-revoked (401),
//   - (_, 0, false) when the header is malformed (silently ignored).
func resolveDevPrincipal(r *http.Request, v string, opts AttachOptions) (Principal, int, bool) {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return Principal{}, 0, false
	}
	kind, raw := parts[0], parts[1]
	id, err := uuid.Parse(raw)
	if err != nil {
		return Principal{}, 0, false
	}
	p := Principal{Kind: kind, ID: kind + ":" + id.String()}

	switch kind {
	case "user":
		if opts.Users != nil {
			u, err := opts.Users.GetByID(r.Context(), id)
			if err != nil {
				return Principal{}, http.StatusUnauthorized, false
			}
			if !u.Active {
				return Principal{}, http.StatusUnauthorized, false
			}
			p.Email = u.Email
		}
		return p, 0, true
	case "agent":
		if opts.Agents != nil {
			a, err := opts.Agents.Get(r.Context(), id)
			if err != nil {
				return Principal{}, http.StatusUnauthorized, false
			}
			if !a.Active {
				return Principal{}, http.StatusUnauthorized, false
			}
		}
		return p, 0, true
	default:
		return Principal{}, 0, false
	}
}
