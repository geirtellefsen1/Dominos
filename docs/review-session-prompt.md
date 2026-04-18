# Independent code review — Dominion MVP

**Instructions for the review session.** Paste this whole document into
a fresh Claude Code session (or any capable review agent) and nothing
else. Do not explain the work beforehand; the point is an independent
read.

---

## Role

You are an independent reviewer. You have not seen this codebase
before. Your only deliverable is a written findings report — do **not**
fix, refactor, commit, or push. Do not run the code either (you'll
likely be in an environment without Docker or a live Postgres, and
live-run findings aren't the goal).

## The project in one sentence

**Dominion** is a self-hostable governance gateway in front of a
sovereign Postgres document store, with first-class AI-agent identities
(X.509 certs), per-document ACLs (OpenFGA), and a signed append-only
audit log. The MVP proves one claim: _every AI action in this system
is attributable, scoped, auditable, and revocable_.

## Where the code lives

- GitHub: `geirtellefsen1/Dominos`
- Branch: `claude/dominion-mvp-architecture-bNsDx`
- Clone + check out:
  ```sh
  git clone https://github.com/geirtellefsen1/Dominos.git
  cd Dominos
  git checkout claude/dominion-mvp-architecture-bNsDx
  ```

## What to read, in this order

1. `CLAUDE.md` — guidance + locked decisions.
2. `docs/architecture.md` — short phase tracker.
3. `README.md` + `packages/api-gateway/README.md` — routes & env knobs.
4. The initial spec. If it is not in `docs/mvp-spec.md` yet, ask the
   operator for the original "Dominion MVP — Architecture & Build
   Spec" document; the codebase is meant to execute it verbatim. The
   key sections are §1 scope, §3 components, §3.4 identity + one-revoke,
   §3.5 agent runtime, §3.7 audit log, §4 hero flow, §5 build phases.
5. `git log --oneline` on the branch — every commit is scoped to one
   phase or one targeted fix, so the history tells the story.
6. The Go code under `packages/api-gateway/internal/` — 11 packages,
   each small and focused. Suggested reading order:
   `db` → `documents` → `auth` → `acl` → `audit` → `agents` → `ca` →
   `graph` → `llm` → `triage` → `queue` → `revoke` → `cors` →
   `cryptokeys` → `scim` → `users` → `httpx`.
7. `packages/agent-runtime/cmd/agent/main.go` — the Phase 5 minimum
   viable agent CLI.
8. `packages/admin-ui/app/page.tsx` — the Phase 9 admin console.
9. `scripts/smoke-test.sh` — the acceptance test for every phase.
10. `infra/docker-compose.yml` + `infra/.env.example` — deployment
    surface.

## What to evaluate

Produce a findings report organised under these headings. Call each
finding **critical** / **high** / **medium** / **low** / **nit**, and
for each one quote the file path + line numbers.

### 1. Governance invariants

The spec's one non-negotiable rule: _every agent tool call must pass
through the Dominion API gateway. Agents never call Postgres, OpenFGA,
or Graph directly._ Verify this holds.

- Can an authenticated agent skip the gateway's auth / ACL / audit
  chain on any route? Especially on the mTLS port `:3443`.
- Is there any path where `r.TLS.PeerCertificates` is trusted without
  a revocation check against the agents store?
- The `documents.Handler` is wrapped with `auth.Require`. Audit does
  the principal appear in **every** audited request's `actor` field
  correctly now? (There was a bug here — commit `c33c1e6` — that
  reversed the audit/auth middleware order; check the fix is complete
  and that no route path bypasses it.)
- One-revoke (`internal/revoke`): after revocation, can a principal
  regain access via a cached session, cached client cert, or a stale
  FGA tuple?

### 2. Authentication + authorization

- `internal/auth/middleware.go`: dev-principal shim is gated by
  `DOMINION_DEV_PRINCIPAL_HEADER`. Is the gate correctly off by
  default in production? What happens if the flag is true and a
  client asserts `user:<random-uuid>` for a user that doesn't exist?
- Session JWTs (`internal/auth/sessions.go`): HS256 secret handling,
  JWT expiration, revocation via DB lookup. Look for missing
  `tok.Valid` checks, algorithm confusion, replay.
- Admin bearer tokens (SCIM, admin, CORS): constant-time comparisons?
  Leakage via error messages or logs?
- OIDC flow (`internal/auth/oidc.go`): state + nonce handling, cookie
  attributes (Secure, SameSite), open-redirect on `return_to`.
- mTLS client-cert path: is thumbprint collision possible? Does the
  CA rotate when the cert regenerates? Spec §3.4 wants CRL updates —
  is the DB `active` flag a sufficient stand-in, and does the
  verification layer actually consult it on every request (not
  cache)?
- OpenFGA check is called on `GET /documents/{id}`. Is it also called
  where the spec expects it — `/me/queue/{id}/approve`,
  `/connectors/graph/callback`, `/admin/triage/run`?

### 3. Audit spine

- `internal/audit/event.go` `canonicalBytes()`: timestamp is
  truncated to µs before sign; confirm. Any other fields that could
  drift across the Postgres JSONB round-trip (numeric precision in
  `context`, Unicode escaping in subject/body, key ordering of
  nested maps)?
- `internal/audit/middleware.go`: is the principal resolution read
  from the inner context the auth middleware put there? Refer to
  commit `c33c1e6` — the middleware order was the bug that made
  every dev-header request log as `actor=anonymous`. Check it really
  is fixed.
- Key rotation: the smoke test filters by `key_id` so old events
  signed by a retired key are skipped. Is there a plan (or a clear
  gap) for verifying across key rotations in production?
- Partition management: `EnsurePartitions(12)` creates the next 12
  months of monthly partitions at boot. What happens at month 13 if
  the gateway hasn't been restarted? Any monitoring hook?

### 4. Secret handling

- `DOMINION_SESSION_SECRET`, `DOMINION_AUDIT_PRIVATE_KEY`,
  `DOMINION_ENCRYPTION_KEY`, `DOMINION_ADMIN_TOKEN`,
  `DOMINION_SCIM_TOKEN`, `DOMINION_CA_KEY_PEM`: each one has a
  fallback when unset. Which fallbacks are safe (random generation
  that breaks restarts loudly) vs. unsafe (silently continuing)?
- `internal/cryptokeys/aesgcm.go`: AES-GCM nonce generation,
  ciphertext format, key length. Any way to produce a malleable or
  forgeable ciphertext? Key rotation story for the encryption key
  (affects stored Graph refresh tokens)?
- `packages/admin-ui/lib/api.ts` stores the admin bearer token in
  `localStorage`. Acceptable for MVP demo; call out as a production
  gap if you think it's worth naming.

### 5. Input validation + SQL

- All DB access goes through `pgx` with placeholders, but look for
  any string-concatenated SQL (the audit `EnsurePartitions` builds
  `CREATE TABLE IF NOT EXISTS %s` — is `from` date-formatted safely?).
- JSON schema validation for `documents` happens in
  `internal/documents/validator.go`. Is the schema cache keyed
  correctly? Can a caller pass a schema id that blows up the
  validator?
- SCIM filter parser accepts `userName eq "..."` only. Are there
  injection-style concerns via the email we then use in a DB lookup?

### 6. Race conditions + error handling

- `internal/graph/poller.go` runs a 60s loop. What happens if the
  previous tick is still running? If the refresh token rotates
  mid-flight? If the DB connection pool is exhausted?
- `internal/triage/triage.go`: two gateway instances running the
  same triage pass — would they duplicate draft creation? What
  happens if the LLM call hangs past the request timeout?
- `internal/queue/queue.go` `approve`: send-mail and status-flip are
  two steps. If Graph accepts the mail but the status update fails,
  what is the user-visible state? Any retry / idempotency?
- `internal/agents/handler.go` revoke: the scim row is flipped to
  inactive before FGA tuples are stripped. If the FGA call fails,
  the handler returns 500; the operator has to re-call DELETE to
  retry the ACL cleanup. Is this documented + correct?

### 7. TLS + CORS

- `internal/cors`: allowlist check, preflight handling, `Vary:
  Origin`. Any reflection-to-`*` path?
- `startTLS` in `main.go` auto-issues a server cert from the
  internal CA on boot if `DOMINION_CA_*` aren't set. What is the
  consequence for already-issued agent certs? The spec says CA
  rotation breaks every agent; is the operator given a clear path
  to persist the CA?

### 8. Test coverage

- Which packages have unit tests? Which don't? Which critical paths
  (e.g. the one-revoke flow, approve-and-send, triage de-dup) are
  _only_ covered by the shell smoke test?
- `scripts/smoke-test.sh` — does it actually assert the invariants
  it claims to (especially for Phase 4's tamper-detection and Phase
  8's post-revoke 401)?
- Any test that depends on a sleep / timing that will flake?

### 9. Operational readiness

- Graceful shutdown: does `main.go` cleanly drain the triage engine,
  graph poller, HTTP and TLS listeners on SIGTERM?
- Observability: `slog` JSON output is present — is there a `context`
  key or request id that a log aggregator could pivot on?
- `/health` endpoint — does it actually check DB + OpenFGA
  connectivity, or is it a bare 200?

### 10. Locked decisions + known gaps

The spec §7 escalations were resolved: Go gateway, OpenFGA, Claude
as default LLM, license deferred. Did the implementation stay inside
those decisions?

Known deferred items (don't count these as findings unless you think
one should actually be done now):

- OpenClaw fork + strip (spec §3.5) — deferred; minimal `cmd/agent`
  binary stands in.
- Admin UI real OIDC session (no browser cookie login yet; admin
  token in `localStorage`).
- `/admin/audit/keys` historical pubkey list — old events signed
  with retired keys can't be verified post-rotation.
- M365 Graph live acceptance — currently exercised via the
  `DOMINION_DEV_GRAPH_SIMULATE` backdoor; no real tenant wiring in
  CI.
- OpenFGA is on the in-memory datastore in compose; ACL tuples vanish
  on `--force-recreate`.
- Cascade revocation (revoking a user does not auto-revoke their
  PAs).

## Output format

Produce markdown with:

```
# Dominion MVP — independent review

## Executive summary
<= 200 words: is the governance claim defensible as built? Biggest
risks?

## Findings
For each:
  ### [severity] one-line title
  **Location:** path:line
  **Observation:** what you saw
  **Impact:** what could go wrong
  **Suggested fix:** a short direction (not code)

## Coverage gaps
List tests that should exist but don't.

## Questions for the human
Anything the code didn't make clear.
```

## Constraints

- Do not rewrite code. Do not commit. Do not run the test suite. Do
  not push to the branch.
- Stop and ask the human if the spec text is not in the repo and you
  need it to evaluate a specific claim.
- Keep speculation separate from observation. If something might be a
  bug but you can't confirm without running the code, label it as a
  question under "Questions for the human".
- Be blunt about severity. A `high` finding is fine; padding the list
  with `nit`s to look thorough is not.
