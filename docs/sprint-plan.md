# Post-review sprint plan

Two sprints to close the independent-review findings from
`docs/review-session-prompt.md`. Sprint 1 unblocks a real demo
(attribution, background-audit, live send). Sprint 2 covers the
durability and hygiene items that matter before an external user
touches the system.

JR deploys once at the end of each sprint. Within a sprint, commits
are bite-sized — one per finding — so any individual change can be
reverted cleanly.

## Sprint 1 — Correctness & Attribution

The four exec-summary findings plus the other high-severity items
and the demo-quality UI polish.

| Order | # | Finding | Severity | Touches |
| :---: | - | ------- | :------: | ------- |
| 1 | 6 | `.env.example` dev flags default-off; `deploy.sh` refuses to start with `DOMINION_DEV_*=true` unless `DOMINION_ENV=development` | medium | `infra/.env.example`, `scripts/deploy.sh` |
| 2 | 7 | Commit the original spec to `docs/mvp-spec.md` so future reviewers don't have to ask | docs | `docs/mvp-spec.md` |
| 3 | 8 | Admin UI: per-tab intros, per-field help, verb+effect button labels, human-readable errors and empty states | UX | `packages/admin-ui/app/page.tsx`, `packages/admin-ui/lib/api.ts` |
| 4 | 1 | `graph.Store.GetForUser` selects + decrypts `refresh_token_ciphertext` so approve-and-send works against a live tenant | **critical** | `packages/api-gateway/internal/graph/store.go` |
| 5 | 2 | `auth.Attach` recognises `Authorization: Bearer` and emits `admin:<token-id>` synthetic principal so admin actions aren't logged as `anonymous` | high | `packages/api-gateway/internal/auth/middleware.go`, all `/admin/*` handlers |
| 6 | 4 | `graph.Ingester.exists` distinguishes `pgx.ErrNoRows` from real DB errors — no more duplicate-ingest on connection hiccups | high | `packages/api-gateway/internal/graph/ingest.go` |
| 7 | 3 | Triage engine, Graph poller, and ingester emit signed audit rows per action with `actor=agent:<id>` / `actor=system:graph-poller` | high | `packages/api-gateway/internal/triage`, `packages/api-gateway/internal/graph`, new direct audit inserts |
| 8 | 5 | Approve flow: intermediate `sending` state + optimistic `status=pending` check + persist real Graph `internetMessageId` | high | `packages/api-gateway/internal/queue`, `graph.Client.SendMail` |

**End-of-sprint proof:**

- Existing smoke test still green (~43 ok lines).
- New Go unit tests: `auth.Attach` resolves admin tokens; ingester returns real errors; approve is idempotent on double-click.
- Audit bundle filtered by `actor=admin:*` shows a row per admin
  action; filtered by `actor=agent:<id>` shows triage + graph
  activity even when no human was driving.

## Sprint 2 — Hardening & Hygiene

Non-blocking for the demo; required before real users.

| Order | # | Finding | Severity | Touches |
| :---: | - | ------- | :------: | ------- |
| 1 | 17 | Delete dead code `httpx.DevPrincipal` | nit | `packages/api-gateway/internal/httpx/httpx.go` |
| 2 | 16 | Cookies: `Secure` attribute when `r.TLS != nil` or `DOMINION_COOKIES_SECURE=true` | low | `packages/api-gateway/internal/auth/oidc.go` |
| 3 | 13 | Audit `context` JSON decoder uses `json.UseNumber()` — no more int→float64 round-trip risk | low | `packages/api-gateway/internal/audit/event.go` |
| 4 | 14 | Smoke test 8c asserts FGA state directly, not just the response count | low | `scripts/smoke-test.sh` |
| 5 | 18 | Document `:3443` dual-auth intent (humans by cookie, agents by cert, same port) | docs | `packages/api-gateway/README.md` |
| 6 | 11 | FGA `ListObjects` continuation-token loop in client + revoke + documents list | medium | `packages/api-gateway/internal/acl/client.go`, `internal/revoke/revoke.go`, `internal/documents/handler.go` |
| 7 | 10 | `DELETE /admin/users/{id}` cascades to all agents where `owner_user_id=id` | medium | `packages/api-gateway/internal/users/handler.go` |
| 8 | 9 | Ephemeral fallbacks refuse to start in `DOMINION_ENV=production` | medium | `packages/api-gateway/internal/ca`, `main.go`, `audit/keys.go`, `cryptokeys/aesgcm.go` |
| 9 | 12 | Graph poller goroutine-per-account with semaphore + exponential back-off on auth failure | low | `packages/api-gateway/internal/graph/poller.go` |
| 10 | 7bis | Audit insert uses a detached context so client disconnect doesn't drop entries | medium | `packages/api-gateway/internal/audit/middleware.go` |
| 11 | 15 | Migrations ledger (`schema_migrations` table) so non-idempotent migrations are safe | medium | `packages/api-gateway/internal/db/db.go`, new migration |

**End-of-sprint proof:**

- All Sprint 1 smoke tests still green.
- New Go tests: migrations skip already-applied, user-cascade revoke removes agent tuples, FGA pagination walks beyond 1000 objects.
- `docker compose up` with `DOMINION_ENV=production` and a missing
  key env var exits non-zero instead of starting.

## Not in scope for either sprint

- Admin UI real OIDC session (replace bearer in `localStorage`).
- `/admin/audit/keys` historical pubkey list.
- M365 Graph live acceptance in CI (requires a test tenant).
- OpenFGA Postgres datastore (currently in-memory in compose).
- OpenClaw upstream fork + strip.

These stay on the v1 backlog per the spec's explicit "not in the MVP"
list (§1.3) and the post-review confirmation that they don't break
the governance claim at MVP scope.

## Deploy gating

After each sprint:
- Push all commits on `claude/dominion-mvp-architecture-bNsDx`.
- Write a short JR prompt (`docs/jr-post-sprintN-deploy-prompt.md`)
  listing the env-file changes, the redeploy invocation, and the new
  smoke-test expectations.
- JR pulls, redeploys, reports. Human verifies the demo flow against
  the updated droplet.
- Sprint N+1 starts only after that verification.
