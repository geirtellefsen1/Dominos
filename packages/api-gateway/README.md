# api-gateway

Single entry point for all client traffic. Every request flows through three
middlewares in order: auth → ACL check → audit tap (spec §3.1).

Phase 1: `/health` + document store & schema registry. Real auth + ACL +
audit middleware lands in phases 2–4.

## Routes (phase 1)

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET    | `/health`          | liveness |
| POST   | `/documents`       | body `{schema_id, body}`; validates against registered JSON schema |
| GET    | `/documents`       | query `?schema=<id>&limit=&offset=` |
| GET    | `/documents/{id}`  | 404 if missing or soft-deleted |

Seeded schemas: `email.v1`, `draft.v1` (see `internal/db/migrations`).

## Env

- `DOMINION_GATEWAY_ADDR` — default `:3000`
- `DOMINION_DATABASE_URL` — default
  `postgres://dominion:dominion_dev@localhost:5432/dominion?sslmode=disable`

## Dev principal shim

Until phase 2 lands real auth, the gateway reads `X-Dominion-Dev-Principal`
from the request and falls back to `dev:anonymous`. Remove this shim when
Phase 2 wires in OIDC / mTLS.

## Run

```sh
go run .
# or from repo root: make gateway
```

Defaults to `:3000`; override with `DOMINION_GATEWAY_ADDR`.
