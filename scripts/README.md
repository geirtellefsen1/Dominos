# Deploy scripts (phases 0–2)

## What's here

| File | Purpose |
| ---- | ------- |
| `deploy.sh`      | Idempotent deploy on the target host. Builds the gateway container, starts docker compose, waits for `/health`, runs smoke tests. |
| `smoke-test.sh`  | Exercises the Phase 1 and Phase 2 acceptance tests via HTTP. |

## Expected host

- Linux with Docker 24+ and the `docker compose` plugin.
- `curl`, `python3`, `openssl` on PATH (all in a default Ubuntu/Debian image).
- Repo checked out at `/opt/dominion` (or anywhere — scripts locate themselves).

## One-shot deploy

```sh
cd /opt/dominion
git checkout claude/dominion-mvp-architecture-bNsDx
git pull --ff-only
scripts/deploy.sh
```

Re-running the script is safe: secrets in `infra/.env` are preserved,
compose services are recreated only when their config changed, the DB
volume persists.

## With the bundled mock IdP (for demo only)

```sh
scripts/deploy.sh --with-mock-oidc
```

This starts `ghcr.io/navikt/mock-oauth2-server` on `:8090`. Edit
`infra/.env` afterwards to point the gateway at it:

```
DOMINION_OIDC_ISSUER=http://mock-oidc:8090/default
DOMINION_OIDC_CLIENT_ID=dominion
DOMINION_OIDC_CLIENT_SECRET=dev
```

Then `scripts/deploy.sh` again to pick up the env changes.

## Smoke-test only (already-running stack)

```sh
DOMINION_PUBLIC_URL=https://dominion.example.com scripts/smoke-test.sh
```
