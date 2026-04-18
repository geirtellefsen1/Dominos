# DEPLOY.md

Canonical deployment guide for Dominion. If you landed here to deploy
anything — first-time or a redeploy — this is the single file to read.
Every other `jr-*-deploy-prompt.md` under `docs/` references back here.

- **Repo:** `geirtellefsen1/dominos`
- **Working branch:** `claude/dominion-mvp-architecture-bNsDx`
- **Target droplet:** `Dominos-Server-01` · `139.59.173.17`
- **Repo path on droplet:** `/opt/dominion`
- **Public surfaces:**
  `http://139.59.173.17:3000` — API gateway ·
  `https://139.59.173.17:3443` — mTLS listener (agents) ·
  `http://139.59.173.17:3100` — web UI ·
  `dominos.tellefsen.org` — same droplet through reverse proxy, if
  configured

---

## Quick commands

Most deploy scenarios are one of these four rows:

| Scenario | Command |
| --- | --- |
| Fresh install on a new droplet | see [First-time deploy](#first-time-deploy) |
| Pull the latest code and redeploy | `cd /opt/dominion && git pull --ff-only && scripts/deploy.sh --with-mock-oidc` |
| Rebuild without pulling (e.g. an env change took effect) | `cd /opt/dominion && scripts/deploy.sh --with-mock-oidc` |
| Full clean slate (destroys postgres + openfga volumes) | `cd /opt/dominion && docker compose --env-file infra/.env -f infra/docker-compose.yml down -v && rm -f infra/.env && scripts/deploy.sh --with-mock-oidc` |

`scripts/deploy.sh` is always idempotent. It generates missing secrets
on first run, rebuilds only what changed, and runs the smoke test at
the end. A failed smoke test leaves the stack up so you can inspect.

---

## Prerequisites on the droplet

Install once. Re-verify if `scripts/deploy.sh` complains.

```sh
docker --version              # 24+
docker compose version        # v2+
git --version
curl --version
python3 --version             # 3.10+
openssl version
python3 -c 'import cryptography; print(cryptography.__version__)'
go version                    # 1.22+  (for the Phase 5 agent-CLI smoke test)
```

Missing anything? On Debian/Ubuntu:

```sh
sudo apt update
sudo apt install -y docker.io docker-compose-plugin git curl \
                    python3 python3-cryptography openssl golang-go
```

If the repo isn't on the droplet yet:

```sh
sudo mkdir -p /opt/dominion
sudo chown "$(whoami):$(whoami)" /opt/dominion
git clone https://github.com/geirtellefsen1/dominos.git /opt/dominion
cd /opt/dominion
git checkout claude/dominion-mvp-architecture-bNsDx
```

---

## First-time deploy

```sh
cd /opt/dominion
scripts/deploy.sh --with-mock-oidc
```

On a clean droplet this takes 5–8 minutes:

1. Creates `infra/.env` from `.env.example`.
2. Generates 6 secrets in-place (session JWT, SCIM token, admin
   token, audit Ed25519 seed, AES-GCM encryption key, postgres
   password).
3. Refuses to proceed if any `DOMINION_DEV_*=true` with
   `DOMINION_ENV != development`. Dev mode is the default in the
   example file — leave it alone unless you're hardening for a
   public-tenant deploy.
4. `docker compose pull` the base images.
5. `docker compose build gateway admin-ui`.
6. `docker compose up -d --force-recreate` — starts five services:
   `postgres`, `openfga`, `mock-oidc`, `gateway`, `admin-ui`.
7. Polls `/health` until it returns `{"status":"ok"}`.
8. Runs `scripts/smoke-test.sh` — expects ~43 green `ok` lines.

Watch for the final line: `deploy complete — stack running at
http://localhost:3000`.

---

## Redeploy (the common case)

Something changed in the repo. Someone merged to the branch. Pull and
redeploy:

```sh
cd /opt/dominion
git fetch origin
git checkout claude/dominion-mvp-architecture-bNsDx
git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
git log -1 --format='%h %s'    # note the head SHA
scripts/deploy.sh --with-mock-oidc
```

What the script handles automatically:

- **Go gateway code change** — compose rebuilds the `gateway` layer
  (~30 s with cached deps).
- **Admin UI / demo UI code change** — compose rebuilds the
  `admin-ui` layer (~2–3 min on first touch because `npm install`
  runs in the build; subsequent rebuilds reuse the cached layer).
- **New migration file under `packages/api-gateway/internal/db/migrations/`**
  — applied on container start, recorded in the
  `schema_migrations` ledger.
- **Env file edit** — `--force-recreate` ensures the running
  container picks up the new value. No manual restart needed.

What the script does **not** handle:

- **Edits to an already-applied migration file.** The ledger refuses
  to start with a checksum mismatch and prints the offending
  filename. Revert the edit, add a new migration instead.
- **Env changes with `DOMINION_ENV=production` set.** The gateway
  refuses to boot if any `DOMINION_DEV_*=true` OR any of the seven
  required key-material vars is empty. See
  [Environment file](#environment-file).

---

## Environment file

The single source of truth is `infra/.env` on the droplet. `deploy.sh`
creates it from `infra/.env.example` on first run and generates the
secrets in-place. Subsequent edits you make by hand are preserved.

### What's in there

| Var | Required | What happens when unset |
| --- | :---: | --- |
| `DOMINION_ENV` | no (defaults to `development`) | Dev-friendly fallbacks on. Set to `production` to gate every dev shim + refuse missing secrets. |
| `DOMINION_SESSION_SECRET` | prod | Gateway auto-generates ephemeral → every session cookie fails verification after restart. |
| `DOMINION_AUDIT_PRIVATE_KEY` | prod | Gateway auto-generates ephemeral → previously-signed audit rows no longer verify. |
| `DOMINION_ENCRYPTION_KEY` | prod | Gateway auto-generates ephemeral → every stored Graph refresh token becomes undecryptable. |
| `DOMINION_CA_CERT_PEM` + `_KEY_PEM` | prod | Gateway generates a fresh CA → every previously-issued agent cert stops authenticating. |
| `DOMINION_ADMIN_TOKEN` | always | `/admin/*` returns 503 admin_disabled. |
| `DOMINION_SCIM_TOKEN` | always | SCIM provisioning rejected. |
| `DOMINION_CORS_ORIGIN` | browser UI | Web UI can't call the gateway from a different origin. |
| `DOMINION_DEV_PRINCIPAL_HEADER` | dev only | Header-based principal shim; used by the smoke test. |
| `DOMINION_DEV_GRAPH_SIMULATE` | dev only | Exposes simulate-ingest endpoint for smoke tests. |
| `DOMINION_DEV_SIMULATE_SEND` | dev only | Approve flow fabricates a Message-ID instead of calling Graph. |
| `DOMINION_GRAPH_CLIENT_ID` + `_CLIENT_SECRET` | live M365 | Polling disabled; simulate path still works. |
| `DOMINION_ANTHROPIC_API_KEY` | real LLM drafts | Triage uses a deterministic stub draft. |
| `DOMINION_TLS_ENABLED` + `_IPS` + `_HOSTNAMES` | agents | `:3443` mTLS listener stays off / cert is localhost-only. |

### Safe edits during the demo

```sh
# enable real Claude for draft generation
echo 'DOMINION_ANTHROPIC_API_KEY=sk-ant-…' >> infra/.env
scripts/deploy.sh --with-mock-oidc
```

```sh
# switch approve to call real Microsoft Graph
sed -i 's|^DOMINION_DEV_SIMULATE_SEND=.*|DOMINION_DEV_SIMULATE_SEND=false|' infra/.env
# fill in DOMINION_GRAPH_CLIENT_ID / _CLIENT_SECRET from Entra
scripts/deploy.sh --with-mock-oidc
```

---

## Demo UI setup (browser)

Post-deploy, the browser UI needs a one-time configuration pass.

1. Browse to `http://139.59.173.17:3100` (or `dominos.tellefsen.org`
   if DNS is in front).
2. Click **Configure demo →**.
3. Fill five fields:

| Field | Value | Where to find it |
| --- | --- | --- |
| Gateway URL | `http://139.59.173.17:3000` or the DNS name | operator knowledge |
| Admin bearer token | the DOMINION_ADMIN_TOKEN | `grep '^DOMINION_ADMIN_TOKEN=' infra/.env` |
| Alice — user id | a uuid | seed via `scripts/smoke-test.sh` and `grep alice= /tmp/smoke.log` |
| Alice — email | `alice@example.com` | cosmetic, anything readable |
| Astrid — agent id | a uuid | seed via `scripts/smoke-test.sh` and `grep 'agent [0-9a-f]' /tmp/smoke.log` |

4. Click **save**.
5. Three panes render (Astrid · Inbox · Audit stream). The
   governance banner starts lighting up within 2 s as the audit
   feed polls.

The config lives in browser `localStorage`, so a hard refresh keeps
it. Clearing the cache resets to the Configure screen.

The old admin dashboard — per-endpoint forms for ACL, SCIM, Audit
export, Triage trigger, Approval queue — still lives at
`http://139.59.173.17:3100/admin`.

---

## Verification checklist

Run after every deploy:

```sh
# Gateway is up
curl -s http://127.0.0.1:3000/health
# → {"status":"ok"}

# TLS listener is up (if DOMINION_TLS_ENABLED=true)
curl -sk https://127.0.0.1:3443/health
# → {"status":"ok"}

# All five services healthy
docker compose --env-file infra/.env -f infra/docker-compose.yml ps

# Migrations ledger recorded every file
docker exec -i dominion-postgres \
    psql -U dominion -c "SELECT name FROM schema_migrations ORDER BY name"

# Admin UI serving
curl -sI http://127.0.0.1:3100/        # demo landing
curl -sI http://127.0.0.1:3100/admin   # admin console

# CORS preflight (from an external host)
curl -si -X OPTIONS http://139.59.173.17:3000/admin/agents \
    -H 'Origin: http://139.59.173.17:3100' \
    -H 'Access-Control-Request-Method: POST' \
    -H 'Access-Control-Request-Headers: authorization,content-type' \
    | head -10
# Expect: HTTP/1.1 204, Access-Control-Allow-Origin: <the origin>
```

---

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `deploy.sh` refuses: "dev-only flags are ENABLED" | `DOMINION_ENV=production` with a dev shim on | either set `DOMINION_ENV=development` for local/demo, or flip the `DOMINION_DEV_*=true` lines to `false` |
| `refusing to start in DOMINION_ENV=production with unset key material` | you flipped to production but didn't fill the seven required keys | run `deploy.sh` once in development first to generate them, then flip |
| Gateway crash-loop | malformed base64 in `DOMINION_SESSION_SECRET` / `AUDIT_PRIVATE_KEY` / `ENCRYPTION_KEY` | clear the value, re-run `deploy.sh` — it regenerates valid base64 |
| Smoke test 4c fails "no events match current key_id" | audit key regenerated because `DOMINION_AUDIT_PRIVATE_KEY` was empty; old rows can't verify | the test's key-id filter handles this; if it still fails, clear the audit_events table or do a full `down -v` |
| Smoke test hangs on `/me/queue` | `DOMINION_DEV_PRINCIPAL_HEADER` is `false` | set to `true` in `infra/.env` and redeploy |
| Admin UI loads, config modal reappears every visit | browser is blocking `localStorage` (Safari incognito) | use a normal window |
| Admin UI throws CORS errors in devtools | `DOMINION_CORS_ORIGIN` doesn't match the browser's origin **exactly** (trailing slash, http vs https) | edit infra/.env, redeploy |
| `admin-ui` container build fails on `npm install` | Next.js pinned a new peer, cache layer broke | `docker compose build --no-cache admin-ui` then re-run `deploy.sh` |
| `migration XXX has been modified since it was applied` | someone edited a .sql file already in the ledger | `git diff` that file, revert the edit, add a new migration if the change is needed |
| `/admin/connectors/graph/simulate` returns 404 | `DOMINION_DEV_GRAPH_SIMULATE` is false | set to true + redeploy (dev only) |
| Triage creates 0 drafts | no recent emails in the lookback window | re-run `scripts/smoke-test.sh` to seed a fresh simulated message |
| Approve returns 502 `send_failed` | real Graph client not configured and `DEV_SIMULATE_SEND` is false | set `DOMINION_DEV_SIMULATE_SEND=true` OR fill in `DOMINION_GRAPH_CLIENT_ID/_SECRET` |

---

## Rollback

Every deploy is a single git commit + a compose rebuild. Roll back:

```sh
cd /opt/dominion
git log --oneline -20           # find the last-known-good SHA
git checkout <sha>
scripts/deploy.sh --with-mock-oidc
```

`--force-recreate` ensures every container goes back to the rolled-
back version. The postgres volume is kept, so audit history + seeded
state survives. When you're ready to re-advance, `git checkout` back
to the branch head.

---

## Rotating secrets

Secrets live in `infra/.env`. Rotating one is a three-step dance:

```sh
# 1. replace the value
sed -i 's|^DOMINION_ADMIN_TOKEN=.*|DOMINION_ADMIN_TOKEN=<new-token>|' infra/.env

# 2. redeploy so the gateway picks up the new value
scripts/deploy.sh --with-mock-oidc

# 3. distribute the new token to every admin client
#    (demo UI: Settings → paste, save)
#    (scripts: update deployments that pass DOMINION_ADMIN_TOKEN=)
```

Rotating `DOMINION_SESSION_SECRET` / `DOMINION_AUDIT_PRIVATE_KEY` /
`DOMINION_ENCRYPTION_KEY` / `DOMINION_CA_*` **invalidates prior
work**:

- Session secret → all active sessions log out.
- Audit key → prior audit rows stop verifying under the new pubkey.
  Export + archive first; then rotate.
- Encryption key → stored Graph refresh tokens are unreadable;
  every connected user must re-consent via `/connectors/graph/connect`.
- CA → every issued agent cert stops authenticating; rotate on a
  deliberate downtime window and re-issue agent certs.

---

## Clean slate (last resort)

Destroys the postgres + openfga volumes. Use only when you actually
want to wipe the droplet's data.

```sh
cd /opt/dominion
docker compose --env-file infra/.env -f infra/docker-compose.yml down -v
rm -f infra/.env
scripts/deploy.sh --with-mock-oidc
```

Takes ~90 s. Re-runs every migration from scratch, generates fresh
secrets, runs the smoke test to re-seed `alice`, `bob`, `astrid` +
the first simulated email. The demo UI config in your browser
(`localStorage`) still points at the old IDs — visit Settings in the
UI and re-paste the fresh IDs from the new smoke log.

---

## What NOT to do

- Don't push to `main` without a human review. The feature branch
  is the flight deck.
- Don't open ports `3000` or `3443` to the public internet without
  a TLS terminator in front — `:3000` is plain HTTP, `:3443` uses a
  private CA no browser trusts.
- Don't hand-edit the `schema_migrations` table. It's the ledger;
  rewriting it means lying about what's been applied.
- Don't run `docker system prune -a` or `docker volume rm` on this
  droplet. The postgres volume carries the audit log.
- Don't `git reset --hard` past a deploy. Stale build artefacts +
  fresh code = debugging nightmare. Use `git checkout <sha>` + a
  clean `deploy.sh` instead.
