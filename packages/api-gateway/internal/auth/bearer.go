package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
)

// RequireBearer is the admin-token middleware factory. It replaces the
// eight near-identical inline auth() methods that used to live in each
// /admin/* handler, and — crucially — it attaches a synthetic
// Principal to the request context on success so the audit tap
// records the admin call as
//
//	actor = admin:<tokenID>
//
// instead of the pre-Sprint-1 `actor = anonymous`. `tokenID` is a
// stable, low-cardinality label we're happy to ship in audit rows:
//
//	"root" — DOMINION_ADMIN_TOKEN (the main admin bearer)
//	"scim" — DOMINION_SCIM_TOKEN  (Entra SCIM provisioning)
//
// Tokens themselves must never appear in logs, audit rows, or the
// request context — only the label does.
//
// Behaviour:
//   - 503 admin_disabled if the configured token is empty
//   - 401 unauthorized  if Authorization: Bearer is missing or wrong
//   - on success, attaches Principal{Kind:"admin", ID:"admin:"+tokenID}
//     and calls next
func RequireBearer(token, tokenID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				httpx.WriteError(w, http.StatusServiceUnavailable, "admin_disabled",
					"bearer token not configured", nil)
				return
			}
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized",
					"invalid bearer token", nil)
				return
			}
			p := Principal{Kind: "admin", ID: "admin:" + tokenID}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}
