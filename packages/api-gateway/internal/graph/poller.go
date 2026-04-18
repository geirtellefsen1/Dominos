package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/audit"
)

// Poller drives the 60s mailbox-polling loop (spec §3.6).
//
// As of Sprint 2 #12 each account runs on its own goroutine guarded
// by a per-account semaphore, so one slow mailbox can't throttle the
// whole tenant, and an account whose refresh token has expired gets
// exponentially backed off instead of burning 1440 fruitless token
// endpoint calls per day.
type Poller struct {
	store    *Store
	client   *Client
	ingester *Ingester
	audit    *audit.Store

	// perAccount maps account.ID -> per-account runtime state (the
	// semaphore that prevents concurrent ticks, plus back-off
	// bookkeeping). Entries are created lazily on first sight.
	perAccount sync.Map // uuid.UUID -> *accountRuntime
}

// accountRuntime holds everything the poller needs to remember
// between ticks for a single connector_account.
type accountRuntime struct {
	mu            sync.Mutex // held while this account is polling
	nextEligible  time.Time  // skip ticks until now >= nextEligible
	consecutiveFail int      // for computing the back-off
}

func NewPoller(s *Store, c *Client, i *Ingester, auditStore *audit.Store) *Poller {
	return &Poller{store: s, client: c, ingester: i, audit: auditStore}
}

func (p *Poller) runtimeFor(id uuid.UUID) *accountRuntime {
	if v, ok := p.perAccount.Load(id); ok {
		return v.(*accountRuntime)
	}
	rt := &accountRuntime{}
	actual, _ := p.perAccount.LoadOrStore(id, rt)
	return actual.(*accountRuntime)
}

// backoffFor returns the next-eligible timestamp after a failure.
// Doubling from 2min up to 30min, capped. isAuth=true (401-shaped
// errors) gets a more aggressive ramp and a longer cap.
func backoffFor(rt *accountRuntime, isAuth bool) time.Duration {
	rt.consecutiveFail++
	base := 2 * time.Minute
	cap := 30 * time.Minute
	if isAuth {
		base = 5 * time.Minute
		cap = 2 * time.Hour
	}
	d := base << min(rt.consecutiveFail-1, 6)
	if d > cap {
		d = cap
	}
	return d
}

// looksLikeAuthFail heuristically identifies a permanent / long
// backoff-worthy error from the token endpoint (vs. a transient
// network blip).
func looksLikeAuthFail(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "invalid_grant") ||
		strings.Contains(s, "401") ||
		strings.Contains(s, "interaction_required") ||
		strings.Contains(s, "aadsts70008") // refresh token expired
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

// tick fires one polling pass. Each account runs on its own
// goroutine; the per-account semaphore (accountRuntime.mu) means a
// still-running tick is skipped rather than piling up a second one
// behind the first.
func (p *Poller) tick(ctx context.Context) {
	accounts, err := p.store.ListActive(ctx)
	if err != nil {
		slog.Error("graph poller list", "err", err)
		return
	}
	var wg sync.WaitGroup
	for _, a := range accounts {
		a := a // capture
		rt := p.runtimeFor(a.ID)

		// Skip accounts still in back-off.
		if time.Now().Before(rt.nextEligible) {
			continue
		}

		// TryLock — if the previous tick for this account is still
		// running (slow mailbox / slow network), skip rather than
		// queue. We'll retry at the next interval.
		if !rt.mu.TryLock() {
			slog.Debug("graph poll skipped — previous tick still running",
				"account_id", a.ID)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer rt.mu.Unlock()

			seen, ingested, err := p.pollOne(ctx, a)
			if err != nil {
				slog.Error("graph poller account", "user_id", a.UserID, "err", err)
				_ = p.store.SetError(ctx, a.ID, err.Error())
				d := backoffFor(rt, looksLikeAuthFail(err))
				rt.nextEligible = time.Now().Add(d)
				slog.Warn("graph poll backing off",
					"account_id", a.ID, "wait", d, "fail_count", rt.consecutiveFail)
			} else {
				rt.consecutiveFail = 0
				rt.nextEligible = time.Time{}
			}
			p.emitPoll(ctx, a, seen, ingested, err)
		}()
	}
	// Bound tick duration to one interval: if any goroutine is still
	// running when the next tick fires, TryLock on the next pass will
	// skip the account. Wait here so failures settle before returning.
	wg.Wait()
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
