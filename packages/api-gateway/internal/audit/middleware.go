package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
)

// Builder lets a handler attach semantic audit metadata (action,
// resource) to the request context; the middleware picks it up when the
// response returns.
type Builder struct {
	Action     string
	Resource   string
	OnBehalfOf string
}

type ctxKey struct{}

func withBuilder(ctx context.Context) (context.Context, *Builder) {
	b := &Builder{}
	return context.WithValue(ctx, ctxKey{}, b), b
}

// From returns the builder attached to the request context; handlers
// call this to set semantic Action/Resource.
func From(ctx context.Context) *Builder {
	b, _ := ctx.Value(ctxKey{}).(*Builder)
	if b == nil {
		return &Builder{}
	}
	return b
}

// statusRecorder captures the response status so the middleware can map
// it into an 'allow' / 'deny' / 'error' decision.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Middleware wraps every request. Returns a middleware that attaches a
// Builder to the context, records the response status, and inserts a
// signed audit event after the handler returns.
//
// Requests that we deliberately skip (health probes, the audit-key
// endpoint itself) are handled inline before the audit tap.
func Middleware(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			ctx, builder := withBuilder(r.Context())
			r = r.WithContext(ctx)
			rec := &statusRecorder{ResponseWriter: w}
			start := time.Now().UTC()

			next.ServeHTTP(rec, r)

			principal, _ := auth.FromContext(r.Context())
			actor := principal.ID
			if actor == "" {
				actor = "anonymous"
			}

			action := builder.Action
			if action == "" {
				action = "http." + r.Method + " " + routeOf(r)
			}

			ev := &Event{
				ID:         uuid.New(),
				Timestamp:  start,
				Actor:      actor,
				OnBehalfOf: builder.OnBehalfOf,
				Action:     action,
				Resource:   builder.Resource,
				Decision:   decisionForStatus(rec.status),
				Context:    encodeContext(r, rec.status, time.Since(start)),
			}

			if err := store.Insert(r.Context(), ev); err != nil {
				// Audit is the governance spine — if it fails we want to
				// know, but the response has already been written to the
				// client. Log loudly; a production deploy should alert
				// on this counter.
				slog.Error("audit insert failed", "err", err,
					"action", ev.Action, "actor", ev.Actor)
			}
		})
	}
}

func shouldSkip(path string) bool {
	switch path {
	case "/health", "/admin/audit/key":
		return true
	}
	return false
}

func routeOf(r *http.Request) string {
	// Best effort without pattern awareness: collapse trailing uuid-ish
	// segments so paths like /documents/<id> bucket together.
	path := r.URL.Path
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if _, err := uuid.Parse(p); err == nil {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

func decisionForStatus(status int) string {
	switch {
	case status == 0:
		return "error"
	case status >= 500:
		return "error"
	case status == http.StatusForbidden, status == http.StatusUnauthorized:
		return "deny"
	case status >= 400:
		return "error"
	default:
		return "allow"
	}
}

type auditContext struct {
	IP        string `json:"ip,omitempty"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	UserAgent string `json:"user_agent,omitempty"`
	DurationMs int64 `json:"duration_ms"`
}

func encodeContext(r *http.Request, status int, dur time.Duration) json.RawMessage {
	ctx := auditContext{
		IP:         clientIP(r),
		Method:     r.Method,
		Path:       r.URL.Path,
		Status:     status,
		UserAgent:  r.UserAgent(),
		DurationMs: dur.Milliseconds(),
	}
	b, err := json.Marshal(ctx)
	if err != nil {
		return nil
	}
	return b
}

func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		if i := strings.Index(x, ","); i >= 0 {
			return strings.TrimSpace(x[:i])
		}
		return strings.TrimSpace(x)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}
