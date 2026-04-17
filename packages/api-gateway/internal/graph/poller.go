package graph

import (
	"context"
	"log/slog"
	"time"
)

// Poller drives the 60s mailbox-polling loop (spec §3.6).
type Poller struct {
	store    *Store
	client   *Client
	ingester *Ingester
}

func NewPoller(s *Store, c *Client, i *Ingester) *Poller {
	return &Poller{store: s, client: c, ingester: i}
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
		if err := p.pollOne(ctx, a); err != nil {
			slog.Error("graph poller account", "user_id", a.UserID, "err", err)
			_ = p.store.SetError(ctx, a.ID, err.Error())
		}
	}
}

func (p *Poller) pollOne(ctx context.Context, a *Account) error {
	tok, err := p.client.Refresh(ctx, a.RefreshToken)
	if err != nil {
		return err
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
		return err
	}

	ingested := 0
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
		"seen", len(messages), "ingested", ingested)

	return p.store.TouchSynced(ctx, a.ID, time.Now().UTC())
}
