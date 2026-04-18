// Package triage implements the Phase 7 Astrid flow: every user with
// an active PA (agent where agents.owner_user_id = user) gets their
// recent email.v1 documents run through Claude; routine ones become
// draft.v1 documents owned by the PA and visible to the user.
package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/auth"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/documents"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/graph"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/httpx"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/llm"
)

type Engine struct {
	pool      *pgxpool.Pool
	documents *documents.Store
	fga       *acl.Client
	llm       *llm.Client

	interval time.Duration
	lookback time.Duration
}

type Options struct {
	Interval time.Duration
	Lookback time.Duration
}

func NewEngine(pool *pgxpool.Pool, docs *documents.Store, fga *acl.Client, llmc *llm.Client, opts Options) *Engine {
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if opts.Lookback <= 0 {
		opts.Lookback = time.Hour
	}
	return &Engine{
		pool:      pool,
		documents: docs,
		fga:       fga,
		llm:       llmc,
		interval:  opts.Interval,
		lookback:  opts.Lookback,
	}
}

// Run blocks until ctx is done, running a pass every `interval`.
func (e *Engine) Run(ctx context.Context) {
	slog.Info("triage engine started", "interval", e.interval, "lookback", e.lookback)
	t := time.NewTicker(e.interval)
	defer t.Stop()
	e.Pass(ctx)
	for {
		select {
		case <-ctx.Done():
			slog.Info("triage engine stopped")
			return
		case <-t.C:
			e.Pass(ctx)
		}
	}
}

// Pass runs one triage cycle across every (user, agent) pair.
func (e *Engine) Pass(ctx context.Context) Stats {
	stats := Stats{}
	pairs, err := e.listPAPairs(ctx)
	if err != nil {
		slog.Error("triage list pa pairs", "err", err)
		return stats
	}
	stats.PairsChecked = len(pairs)
	for _, p := range pairs {
		processed, drafted, skipped, err := e.triageForPair(ctx, p)
		if err != nil {
			slog.Error("triage pair", "user", p.UserID, "agent", p.AgentID, "err", err)
			stats.Errors++
			continue
		}
		stats.EmailsProcessed += processed
		stats.DraftsCreated += drafted
		stats.EmailsSkipped += skipped
	}
	slog.Info("triage pass", "pairs", stats.PairsChecked,
		"emails", stats.EmailsProcessed, "drafts", stats.DraftsCreated,
		"skipped", stats.EmailsSkipped, "errors", stats.Errors)
	return stats
}

type Stats struct {
	PairsChecked    int `json:"pairs_checked"`
	EmailsProcessed int `json:"emails_processed"`
	DraftsCreated   int `json:"drafts_created"`
	EmailsSkipped   int `json:"emails_skipped"`
	Errors          int `json:"errors"`
}

type paPair struct {
	UserID  uuid.UUID
	AgentID uuid.UUID
}

func (e *Engine) listPAPairs(ctx context.Context) ([]paPair, error) {
	const q = `
        SELECT a.owner_user_id, a.id
        FROM agents a
        WHERE a.active = TRUE
          AND a.owner_user_id IS NOT NULL
    `
	rows, err := e.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []paPair
	for rows.Next() {
		var p paPair
		if err := rows.Scan(&p.UserID, &p.AgentID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// triageForPair pulls recent email.v1 documents that don't yet have a
// corresponding draft.v1 and runs the PA over each one.
func (e *Engine) triageForPair(ctx context.Context, p paPair) (processed, drafted, skipped int, err error) {
	const q = `
        SELECT e.id, e.body
        FROM documents e
        WHERE e.schema_id = 'email.v1'
          AND e.deleted_at IS NULL
          AND e.created_at >= $1
          AND e.created_by = $2
          AND NOT EXISTS (
              SELECT 1 FROM documents d
              WHERE d.schema_id = 'draft.v1'
                AND d.deleted_at IS NULL
                AND d.body->>'inReplyToDocumentId' = e.id::text
                AND d.body->>'generatedByAgent' = $3
          )
        ORDER BY e.created_at ASC
        LIMIT 25
    `
	since := time.Now().Add(-e.lookback)
	userPrincipal := "user:" + p.UserID.String()
	agentPrincipal := "agent:" + p.AgentID.String()

	rows, err := e.pool.Query(ctx, q, since, userPrincipal, agentPrincipal)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	type emailRow struct {
		ID   uuid.UUID
		Body []byte
	}
	var batch []emailRow
	for rows.Next() {
		var r emailRow
		if err := rows.Scan(&r.ID, &r.Body); err != nil {
			return 0, 0, 0, err
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}

	for _, r := range batch {
		processed++
		var email graph.EmailBody
		if err := json.Unmarshal(r.Body, &email); err != nil {
			slog.Warn("triage parse email", "id", r.ID, "err", err)
			continue
		}
		prompt := buildPrompt(email)
		reply, skipReason, err := e.llm.DraftReply(ctx, prompt)
		if err != nil {
			slog.Warn("triage llm", "id", r.ID, "err", err)
			continue
		}
		if skipReason != "" {
			skipped++
			continue
		}
		subject := email.Subject
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
		draftBody := map[string]any{
			"inReplyToDocumentId": r.ID.String(),
			"to":                  []string{email.From},
			"subject":             subject,
			"body":                reply,
			"generatedByAgent":    agentPrincipal,
			"generatedAt":         time.Now().UTC().Format(time.RFC3339),
			"status":              "pending",
		}
		raw, _ := json.Marshal(draftBody)
		doc, err := e.documents.Create(ctx, "draft.v1", raw, agentPrincipal)
		if err != nil {
			slog.Warn("triage create draft", "id", r.ID, "err", err)
			continue
		}
		// Agent owns the draft; user sees it via a reader tuple.
		tuples := []acl.Tuple{
			{User: agentPrincipal, Relation: acl.RelOwner, Object: "document:" + doc.ID.String()},
			{User: userPrincipal, Relation: acl.RelReader, Object: "document:" + doc.ID.String()},
			{User: userPrincipal, Relation: acl.RelWriter, Object: "document:" + doc.ID.String()},
		}
		if e.fga != nil {
			if err := e.fga.Write(ctx, tuples...); err != nil {
				slog.Warn("triage fga grant", "id", r.ID, "err", err)
				continue
			}
		}
		drafted++
	}
	return processed, drafted, skipped, nil
}

func buildPrompt(email graph.EmailBody) string {
	return fmt.Sprintf(`You are Astrid, an attentive personal email assistant. Read the email below and produce a short, polite reply draft.

If the email is automated (receipts, marketing, notifications, newsletters, calendar invites, out-of-office), respond with exactly one line starting with "SKIP: " followed by a short reason. Do NOT draft a reply for those.

Otherwise respond with ONLY the body of the reply — no subject line, no salutation if it's redundant, no signature. Keep it under 120 words.

Email:
From: %s
To: %s
Subject: %s
Received: %s

---
%s`,
		email.From,
		strings.Join(email.To, ", "),
		email.Subject,
		email.ReceivedAt,
		firstNonEmpty(email.BodyText, email.BodyHtml))
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// --- HTTP: POST /admin/triage/run -----------------------------------------

type Handler struct {
	engine *Engine
	token  string
}

func NewHandler(e *Engine, token string) *Handler { return &Handler{engine: e, token: token} }

func (h *Handler) Register(mux *http.ServeMux) {
	admin := auth.RequireBearer(h.token, "root")
	mux.Handle("POST /admin/triage/run", admin(http.HandlerFunc(h.run)))
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	stats := h.engine.Pass(r.Context())
	httpx.WriteJSON(w, http.StatusOK, stats)
}
