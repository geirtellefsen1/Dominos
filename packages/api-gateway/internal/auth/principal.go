package auth

import (
	"context"
	"net/http"
)

// Principal is the authenticated actor attached to a request by the auth
// middleware. Phase 2 populates `user:<uuid>` principals from OIDC-backed
// sessions. Phase 5 will extend this to `agent:<id>` principals backed by
// mTLS client certificates.
type Principal struct {
	Kind  string // "user" | "agent" | "dev"
	ID    string // e.g. "user:019..." or "dev:anonymous"
	Email string
}

func (p Principal) String() string { return p.ID }

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// FromRequest returns the principal attached to the request. Callers that
// require authentication should use RequireAuth middleware; this helper is
// for logging and optional-principal code paths.
func FromRequest(r *http.Request) Principal {
	if p, ok := FromContext(r.Context()); ok {
		return p
	}
	return Principal{Kind: "dev", ID: "dev:anonymous"}
}
