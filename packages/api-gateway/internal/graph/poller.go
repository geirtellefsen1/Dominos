package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/audit"
)

// Poller drives the 60s mailbox-polling loop (spec §3.6).
type Poller struct {
	store    *Store
	client   *Client
	ingester *Ingester
	audit    *audit.Store
}

func NewPoller(s *Store, c *Client, i *Ingester, auditStore *audit.Store) *Poller {
	return &Poller{store: s, client: c, ingester: i, audit: auditStore}
}

// emitPoll records one poll attempt per (account, tick) — success or
// failure — so the audit log shows the background Graph traffic.
func (p *Poller) emitPoll(ctx context.Context, a *Account, seen, ingested int, pollErr error) {
	if p.audit == nil {
		return
	}
	decision := "allow"
	if pollErr != nil {
		decision = "error"
	}
	body := map[string]any{
		"account_id":   a.ID.String(),
		"mailbox_user": a.ExternalAccountID,
		"seen":         seen,
		"ingested":     ingested,
	}
	if pollErr != nil {
		body["error"] = pollErr.Error()
	}
	ctxJSON, _ := json.Marshal(body)
	ev := &audit.Event{
		ID:         uuid.New(),
		Timestamp:  time.Now(),
		Actor:      "system:graph-poller",
		OnBehalfOf: "user:" + a.UserID.String(),
		Action:     "graph.poll",
		Resource:   "connector_account:" + a.ID.String(),
		Decision:   decision,
		Context:    ctxJSON,
	}
	if err := p.audit.Insert(ctx, ev); err != nil {
		slog.Error("graph poll audit insert", "err", err)
	}
}

// Run blocks until ctx is done.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("graph poller started", "interval", PollerInterval)
	p.tick(ctx)
	t := time.NewTicker(PollerInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("graph poller stopped")
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	accounts, err := p.store.ListActive(ctx)
	if err != nil {
		slog.Error("graph poller list", "err", err)
		return
	}
	for _, a := range accounts {
		seen, ingested, err := p.pollOne(ctx, a)
		if err != nil {
			slog.Error("graph poller account", "user_id", a.UserID, "err", err)
			_ = p.store.SetError(ctx, a.ID, err.Error())
		}
		p.emitPoll(ctx, a, seen, ingested, err)
	}
}

func (p *Poller) pollOne(ctx context.Context, a *Account) (seen, ingested int, err error) {
	tok, err := p.client.Refresh(ctx, a.RefreshToken)
	if err != nil {
		return 0, 0, err
	}
	// Rotate refresh token if Microsoft issued a new one.
	if tok.RefreshToken != "" && tok.RefreshToken != a.RefreshToken {
		if _, err := p.store.Upsert(ctx, a.UserID, a.ExternalAccountID, tok.RefreshToken); err != nil {
			slog.Warn("rotate refresh token", "err", err)
		}
	}

	since := time.Time{}
	if a.LastSyncedAt != nil {
		since = *a.LastSyncedAt
	}
	messages, err := p.client.ListMessages(ctx, tok.AccessToken, since, 25)
	if err != nil {
		return 0, 0, err
	}
	seen = len(messages)

	for _, m := range messages {
		body := MessageToEmailBody(m, a.ExternalAccountID)
		doc, err := p.ingester.Ingest(ctx, a.UserID, body)
		if err != nil {
			slog.Warn("ingest message", "message_id", m.ID, "err", err)
			continue
		}
		if doc != nil {
			ingested++
		}
	}
	slog.Info("graph poll ok",
		"user_id", a.UserID, "mailbox", a.ExternalAccountID,
		"seen", seen, "ingested", ingested)

	if err := p.store.TouchSynced(ctx, a.ID, time.Now().UTC()); err != nil {
		return seen, ingested, err
	}
	return seen, ingested, nil
}
