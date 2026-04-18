package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists signed audit events and exposes a bounded query API.
type Store struct {
	pool *pgxpool.Pool
	keys *Keys
}

func NewStore(pool *pgxpool.Pool, keys *Keys) *Store {
	return &Store{pool: pool, keys: keys}
}

func (s *Store) Keys() *Keys { return s.keys }

// EnsurePartitions creates the current month and the next `ahead` months
// as PARTITION OF audit_events. Idempotent — safe to run on every boot.
// Each month is one partition; at the end of every year run this again
// (or let the nightly boot do it) to extend the runway.
func (s *Store) EnsurePartitions(ctx context.Context, ahead int) error {
	if ahead < 1 {
		ahead = 12
	}
	start := firstOfMonth(time.Now().UTC())
	for i := 0; i < ahead; i++ {
		from := start.AddDate(0, i, 0)
		to := from.AddDate(0, 1, 0)
		name := fmt.Sprintf("audit_events_%s", from.Format("200601"))
		stmt := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF audit_events
             FOR VALUES FROM ('%s') TO ('%s')`,
			name,
			from.Format(time.RFC3339),
			to.Format(time.RFC3339),
		)
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("create partition %s: %w", name, err)
		}
	}
	slog.Info("audit partitions ensured", "months", ahead, "from", start.Format("2006-01"))
	return nil
}

// Insert signs and writes the event. Returns the signed Event for logging.
func (s *Store) Insert(ctx context.Context, e *Event) error {
	// Postgres TIMESTAMPTZ is microsecond-precision. If we sign a
	// nanosecond-precision time.Time and then round-trip through the
	// DB, the exported value will have been truncated to microseconds,
	// canonicalBytes() will produce a different string, and Ed25519
	// verification will fail systematically. Truncate BEFORE signing
	// so the signed bytes match what Query() will return.
	e.Timestamp = e.Timestamp.UTC().Truncate(time.Microsecond)
	e.KeyID = s.keys.KeyID
	if err := e.Sign(s.keys.Private); err != nil {
		return fmt.Errorf("sign event: %w", err)
	}
	const q = `
        INSERT INTO audit_events
            (id, timestamp, actor, on_behalf_of, action, resource, decision, context, signature, key_id)
        VALUES ($1, $2, $3, NULLIF($4,''), $5, NULLIF($6,''), $7, $8, $9, $10)
    `
	_, err := s.pool.Exec(ctx, q,
		e.ID, e.Timestamp, e.Actor, e.OnBehalfOf,
		e.Action, e.Resource, e.Decision, e.Context,
		e.Signature, e.KeyID,
	)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

// QueryParams filters an audit export (spec §3.7: from, to, actor).
type QueryParams struct {
	From  time.Time
	To    time.Time
	Actor string
	Limit int
}

func (s *Store) Query(ctx context.Context, p QueryParams) ([]*Event, error) {
	if p.Limit <= 0 || p.Limit > 10000 {
		p.Limit = 1000
	}
	if p.To.IsZero() {
		p.To = time.Now().UTC().Add(time.Minute)
	}
	if p.From.IsZero() {
		p.From = p.To.AddDate(0, -1, 0)
	}
	const q = `
        SELECT id, timestamp, actor, COALESCE(on_behalf_of,''), action,
               COALESCE(resource,''), decision, context, signature, key_id
        FROM audit_events
        WHERE timestamp >= $1 AND timestamp < $2
          AND ($3 = '' OR actor = $3)
        ORDER BY timestamp ASC
        LIMIT $4
    `
	rows, err := s.pool.Query(ctx, q, p.From, p.To, p.Actor, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		e := &Event{}
		var ctxRaw []byte
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.Actor, &e.OnBehalfOf,
			&e.Action, &e.Resource, &e.Decision, &ctxRaw,
			&e.Signature, &e.KeyID,
		); err != nil {
			return nil, err
		}
		if len(ctxRaw) > 0 {
			e.Context = json.RawMessage(ctxRaw)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
