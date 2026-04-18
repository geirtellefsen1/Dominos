# JR — deploy Sprint 2 to `Dominos-Server-01`

Paste this whole prompt into JR. Sprint 2 is the post-review hardening
batch — no new user-visible features, eleven targeted fixes that make
the existing features safer.

## Server

- Name: `Dominos-Server-01`
- Public IPv4: `139.59.173.17`
- Repo path on droplet: `/opt/dominion`
- Branch: `claude/dominion-mvp-architecture-bNsDx`

## What changed since the Sprint 1 deploy

11 commits, all on the branch head. Bite-sized so each one can be
bisected / reverted on its own.

1. `679650f` `sprint2/#17` — delete dead `httpx.DevPrincipal`.
2. `dd1e399` `sprint2/#16` — cookies get the `Secure` attribute when
   `r.TLS != nil` OR `DOMINION_COOKIES_SECURE=true` is set.
3. `04d4e50` `sprint2/#13` — audit canonical form uses
   `json.UseNumber` so large ints don't silently round-trip through
   `float64`.
4. `01e38b4` `sprint2/#14` — smoke 8c now re-grants the revoked
   agent's tuple and asserts the agent still gets 401, proving the
   earlier revoke really reached FGA.
5. `61b9c42` `sprint2/#18` — the `:3443` dual-auth layout is
   documented in `packages/api-gateway/README.md`.
6. `9becad9` `sprint2/#11` — revoke uses `/stores/{id}/read` with
   `continuation_token` pagination. Large mailboxes with >1000 tuples
   are fully cleared on offboarding now.
7. `9639473` `sprint2/#10` — `DELETE /admin/users/{id}` cascades to
   every agent where `owner_user_id` = the user. Response now carries
   a `cascaded_agents[]` array.
8. `9e0ab63` `sprint2/#9` — `DOMINION_ENV=production` refuses to
   start if any of seven key env vars is unset.
9. `3b1c40e` `sprint2/#12` — Graph poller runs one goroutine per
   account with a `TryLock` semaphore and exponential back-off on
   auth failures.
10. `529a82e` `sprint2/#7bis` — audit insert uses a detached 5s
    context so a client disconnect can't drop the row.
11. `9428a9a` `sprint2/#15` — `schema_migrations` ledger +
    `pg_advisory_lock` so two parallel gateways can't race on
    migrations and an already-applied file isn't re-executed.

## Task

Pull the branch, redeploy, confirm the smoke test passes, and spot-check
the three observable behaviour changes. Stop and ask if anything looks
off.

## Steps

### 1. Connect + update

```sh
ssh dominos-server
cd /opt/dominion
git fetch origin
git checkout claude/dominion-mvp-architecture-bNsDx
git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
git log -1 --format='%h %s'
```

Expect the head commit to be `sprint2/#15: schema_migrations ledger`
or later. If the pull is not a fast-forward, **stop and report**.

### 2. Sanity-check infra/.env

No edits required — the droplet is in development mode and the seven
keys `deploy.sh` generates are already populated. Confirm:

```sh
grep -E '^(DOMINION_ENV|DOMINION_COOKIES_SECURE)=' infra/.env
```

Expect:

```
DOMINION_ENV=development
```

`DOMINION_COOKIES_SECURE` will be absent (empty) — that's fine on
the current plain-HTTP droplet. Leave it alone.

### 3. Deploy

```sh
scripts/deploy.sh --with-mock-oidc
```

`deploy.sh` is idempotent and already `--force-recreate`s the
gateway. Expect 1–2 min of rebuild (Go cached) and the smoke test to
finish with `all smoke tests passed (phases 1, 2, 3, 4, 5, 6, 7, 8)`
— ~43 green `ok` lines, zero `fail` lines.

### 4. Verify the three observable changes

Open a separate SSH window or use the admin UI at
`http://139.59.173.17:3100`.

**4a. Migrations ledger populated.** This is the only change to the
DB schema shape itself.

```sh
docker exec -i dominion-postgres psql -U dominion -c \
    "SELECT name, applied_at FROM schema_migrations ORDER BY name"
```

Expect 7 rows, one per `.sql` file in the repo
(`001_init.sql` through `007_draft_status_sending.sql`). All
`applied_at` timestamps will be within the last minute (the first
boot after the pull recorded everything at once — back-compat
bootstrap, not a bug).

**4b. User revoke cascades.** Run the smoke test's Phase 8 fixtures
(or from the admin UI Users tab):

```sh
curl -s -X DELETE -H "Authorization: Bearer $(grep ^DOMINION_ADMIN_TOKEN= infra/.env | cut -d= -f2)" \
    http://127.0.0.1:3000/admin/users/<some-user-uuid> | jq .
```

Expect the response to include:

```json
{
  "user":            { ..., "active": false, ... },
  "sessions_revoked": 0,
  "tuples_removed":   3,
  "cascaded_agents":  [ { "agent_id": "...", "tuples_removed": N } ]
}
```

If the user had no owned agents, `cascaded_agents` is an empty
array — not missing.

**4c. Poller goroutine isolation + back-off.** Tail the gateway
logs for a minute and confirm you see either `graph poll ok` per
account (happy path) or `graph poll backing off` with a `wait:
<duration>` (if a connector has a stale refresh token). Either is
healthy; what you're checking for is **the absence of
`graph poller account ... err ...` messages firing every 60s**
without any backoff message.

```sh
docker compose --env-file infra/.env -f infra/docker-compose.yml logs \
    --since=5m gateway | grep -E 'graph poll|backing off' | tail -20
```

## Report back

One reply with:

1. Commit SHA at HEAD: `git log -1 --format=%H`.
2. Tail of the smoke-test output — last 40 lines. Expect every
   Phase 1–8 check green, including the new 8c assertion.
3. The 7 rows from `SELECT ... FROM schema_migrations`.
4. `docker compose ps` showing five healthy containers.
5. Any warning in the gateway logs during the 5-minute observation
   window.

## Do NOT

- Do not commit, push, or modify the branch. All fixes land via
  `git pull` from the operator's workstation.
- Do not set `DOMINION_ENV=production` on this droplet. We're in
  development mode because the smoke test needs the dev-principal
  header and the simulate backdoors. `production` would refuse to
  start.
- Do not enable `DOMINION_COOKIES_SECURE=true` on the current
  plain-HTTP deploy — it would mark cookies Secure, and the browser
  wouldn't send them back over HTTP, breaking the admin UI session
  tab. Reserved for when a real TLS terminator is in front.
- Do not run `docker compose down -v`. The schema_migrations ledger
  and the audit log both live in the postgres volume; Sprint 2's
  cascade-revoke + migration bootstrap behaviour is most visibly
  correct on a DB that has prior state.
- Do not edit migration files after they have been applied. The
  ledger checksum will refuse to start on next boot with an error
  like `migration 007_... has been modified since it was applied`.
  Correct fix if a migration is ever wrong: add a new migration file
  that forward-patches the schema.

## If something fails

| Symptom | What to do |
| --- | --- |
| `refusing to start in DOMINION_ENV=production with unset key material` | someone set DOMINION_ENV=production; revert to `development` in `infra/.env` and redeploy |
| `migration XXX has been modified since it was applied` | a .sql file was edited after first apply; `git diff` + revert the edit, do not try to patch the ledger by hand |
| gateway logs `graph poll backing off` on every tick for the same account for >30 min | the connector's refresh token is truly expired; have the user re-consent via `/connectors/graph/connect` |
| smoke test fails on 8c with HTTP 200 instead of 401 | something in Sprint 1 regressed `auth.Attach`'s agent-active check — capture the log and report |
| smoke test fails on 8c with 409 on the re-grant | the tuple was not actually deleted during 8a revoke — acl client / revoke code bug; capture the admin/acl/revoke response body and report |
