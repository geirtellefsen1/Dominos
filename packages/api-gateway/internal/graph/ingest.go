package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/documents"
)

// EmailBody is the JSON that populates the email.v1 schema. Matches
// internal/db/migrations/002_seed_schemas.sql.
type EmailBody struct {
	MessageID   string            `json:"messageId"`
	MailboxUser string            `json:"mailboxUser"`
	From        string            `json:"from"`
	To          []string          `json:"to"`
	Cc          []string          `json:"cc,omitempty"`
	Subject     string            `json:"subject"`
	BodyText    string            `json:"bodyText,omitempty"`
	BodyHtml    string            `json:"bodyHtml,omitempty"`
	ReceivedAt  string            `json:"receivedAt"`
	Headers     map[string]string `json:"headers,omitempty"`
}

func MessageToEmailBody(m Message, mailbox string) EmailBody {
	body := EmailBody{
		MessageID:   m.ID,
		MailboxUser: mailbox,
		From:        m.From.EmailAddress.Address,
		Subject:     m.Subject,
		ReceivedAt:  m.ReceivedDateTime,
	}
	for _, r := range m.ToRecipients {
		body.To = append(body.To, r.EmailAddress.Address)
	}
	for _, r := range m.CcRecipients {
		body.Cc = append(body.Cc, r.EmailAddress.Address)
	}
	switch m.Body.ContentType {
	case "html":
		body.BodyHtml = m.Body.Content
		body.BodyText = m.BodyPreview
	default:
		body.BodyText = m.Body.Content
		if body.BodyText == "" {
			body.BodyText = m.BodyPreview
		}
	}
	return body
}

// Ingester turns Graph messages into governed email.v1 documents:
//
//  1. Insert into `documents` with created_by = user (so the user is the
//     row's author of record).
//  2. Write FGA owner tuple: document:<id>#owner@user:<id>.
//  3. If the user has any agents (agents.owner_user_id = user_id),
//     write document:<id>#reader@agent:<id> for each — this is the
//     mechanism by which the user's PA gets inbox visibility (§1.2).
//  4. Caller is responsible for recording an audit event (the HTTP
//     middleware handles that for the /admin/connectors/graph/simulate
//     path; the background poller logs per-batch metrics).
type Ingester struct {
	pool      *pgxpool.Pool
	documents *documents.Store
	fga       *acl.Client
}

func NewIngester(pool *pgxpool.Pool, docs *documents.Store, fga *acl.Client) *Ingester {
	return &Ingester{pool: pool, documents: docs, fga: fga}
}

// Ingest writes one email.v1 document and returns the inserted row.
// Duplicate messageIds for the same mailbox are skipped (returns nil doc, nil err).
func (i *Ingester) Ingest(ctx context.Context, userID uuid.UUID, body EmailBody) (*documents.Document, error) {
	exists, err := i.exists(ctx, body.MailboxUser, body.MessageID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, nil
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	principal := "user:" + userID.String()
	doc, err := i.documents.Create(ctx, "email.v1", raw, principal)
	if err != nil {
		return nil, fmt.Errorf("create document: %w", err)
	}

	if i.fga != nil {
		tuples := []acl.Tuple{{
			User:     principal,
			Relation: acl.RelOwner,
			Object:   "document:" + doc.ID.String(),
		}}
		paIDs, err := i.userAgents(ctx, userID)
		if err != nil {
			slog.Warn("list user agents", "err", err)
		}
		for _, pid := range paIDs {
			tuples = append(tuples, acl.Tuple{
				User:     "agent:" + pid.String(),
				Relation: acl.RelReader,
				Object:   "document:" + doc.ID.String(),
			})
		}
		if err := i.fga.Write(ctx, tuples...); err != nil {
			return nil, fmt.Errorf("write fga tuples: %w", err)
		}
	}
	return doc, nil
}

func (i *Ingester) exists(ctx context.Context, mailbox, messageID string) (bool, error) {
	const q = `
        SELECT 1 FROM documents
        WHERE schema_id = 'email.v1'
          AND deleted_at IS NULL
          AND body->>'messageId' = $1
          AND body->>'mailboxUser' = $2
        LIMIT 1
    `
	var one int
	err := i.pool.QueryRow(ctx, q, messageID, mailbox).Scan(&one)
	if err != nil {
		// pgx.ErrNoRows falls through as err != nil but not a real error.
		return false, nil //nolint:nilerr // treated as "doesn't exist"
	}
	return true, nil
}

func (i *Ingester) userAgents(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := i.pool.Query(ctx,
		`SELECT id FROM agents WHERE owner_user_id = $1 AND active = TRUE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PollerInterval is the spec-mandated 60s cadence.
const PollerInterval = 60 * time.Second
