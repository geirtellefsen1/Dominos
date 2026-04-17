# api-gateway

Single entry point for all client traffic. Every request flows through three
middlewares in order: auth → ACL check → audit tap (spec §3.1).

Phase 1: `/health` + document store & schema registry. Real auth + ACL +
audit middleware lands in phases 2–4.

## Routes (phase 1–2)

| Method | Path                    | Notes |
| ------ | ----------------------- | ----- |
| GET    | `/health`               | liveness |
| GET    | `/auth/login`           | OIDC redirect to IdP; accepts `?return_to=` |
| GET    | `/auth/callback`        | OIDC code exchange; sets session cookie |
| POST   | `/auth/logout`          | revokes the current session, clears cookie |
| GET    | `/me`                   | authenticated user's profile (401 without session) |
| POST   | `/documents`            | body `{schema_id, body}`; validates against schema |
| GET    | `/documents`            | query `?schema=<id>&limit=&offset=` |
| GET    | `/documents/{id}`       | 404 if missing or soft-deleted |
| POST   | `/scim/v2/Users`        | create (Bearer `DOMINION_SCIM_TOKEN`) |
| GET    | `/scim/v2/Users?filter` | supports `userName eq "<email>"` |
| GET    | `/scim/v2/Users/{id}`   | fetch |
| PATCH  | `/scim/v2/Users/{id}`   | supports `active` toggle; deactivation revokes sessions |
| PUT    | `/scim/v2/Users/{id}`   | full replace; deactivation revokes sessions |
| DELETE | `/scim/v2/Users/{id}`   | treated as deactivate |

Seeded schemas: `email.v1`, `draft.v1` (see `internal/db/migrations`).

## Env

| Var | Default | Notes |
| --- | ------- | ----- |
| `DOMINION_GATEWAY_ADDR`      | `:3000` | |
| `DOMINION_DATABASE_URL`      | `postgres://dominion:dominion_dev@localhost:5432/dominion?sslmode=disable` | |
| `DOMINION_SESSION_SECRET`    | random per run | HMAC key for session JWTs; set in prod |
| `DOMINION_OIDC_ISSUER`       | unset | e.g. `http://localhost:8090/default` for the mock IdP |
| `DOMINION_OIDC_CLIENT_ID`    | unset | |
| `DOMINION_OIDC_CLIENT_SECRET`| unset | |
| `DOMINION_OIDC_REDIRECT_URL` | `http://localhost:3000/auth/callback` | |
| `DOMINION_SCIM_TOKEN`        | unset | Bearer token that SCIM clients must present |

## Dev shims

Until the full governance middleware lands in Phase 3 / 4, the gateway reads
`X-Dominion-Dev-Principal` as a fallback on routes that don't yet require a
real session. This header is dev-only and is removed in Phase 3.

## Run

```sh
go run .
# or from repo root: make gateway
```

Defaults to `:3000`; override with `DOMINION_GATEWAY_ADDR`.
