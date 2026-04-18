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
| POST   | `/admin/acl/grant`      | bearer `DOMINION_ADMIN_TOKEN`; body `{principal,relation,resource}` |
| POST   | `/admin/acl/revoke`     | bearer `DOMINION_ADMIN_TOKEN`; body `{principal,relation,resource}` |
| GET    | `/admin/audit`          | bearer `DOMINION_ADMIN_TOKEN`; query `from`, `to`, `actor`; returns signed bundle + pubkey |
| GET    | `/admin/audit/key`      | Ed25519 pubkey used to sign the log (for offline verification) |
| POST   | `/admin/agents`         | bearer token; body `{display_name, owner_user_id?}`; returns cert + key PEM (once) |
| DELETE | `/admin/agents/{id}`    | bearer token; one-revoke: flips `active=false` **and** strips every FGA tuple where `agent:<id>` is a subject |
| DELETE | `/admin/users/{id}`     | bearer token; one-revoke: deactivates user, revokes all sessions, strips every FGA tuple where `user:<id>` is a subject |
| GET    | `/admin/ca/cert`        | unauthenticated; returns Dominion CA cert PEM so callers can verify the TLS listener |
| GET    | `/connectors/graph/connect`  | authenticated user; redirects to the Microsoft consent page |
| GET    | `/connectors/graph/callback` | OAuth callback; exchanges code + stores refresh token |
| POST   | `/admin/connectors/graph/simulate` | dev-only; bearer token; injects a fake Graph message through the real ingest pipeline (gated by `DOMINION_DEV_GRAPH_SIMULATE=true`) |
| GET    | `/me/inbox`              | authenticated user's `email.v1` documents, newest first, ACL-filtered |
| GET    | `/me/queue`              | authenticated user's pending `draft.v1` documents (approval queue) |
| POST   | `/me/queue/{id}/approve` | send via Graph `/me/sendMail` (or simulate), mark draft `sent` |
| POST   | `/me/queue/{id}/reject`  | mark draft `rejected` without sending |
| POST   | `/admin/triage/run`      | bearer token; run one triage pass immediately across every active PA |

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
| `DOMINION_ADMIN_TOKEN`       | unset | Bearer token for `/admin/*` routes (phase 3+) |
| `DOMINION_AUDIT_PRIVATE_KEY` | random per run | base64 Ed25519 seed (32 bytes) or private key (64 bytes) used to sign audit rows. Set in prod so exports keep verifying across restarts. |
| `DOMINION_TLS_ENABLED`       | `false` | When `true`, also listen on `:3443` with mTLS; accepts agent client certs. |
| `DOMINION_GATEWAY_TLS_ADDR`  | `:3443` | TLS bind address. |
| `DOMINION_TLS_HOSTNAMES`     | unset | Comma-separated extra DNS names for the auto-issued server cert. |
| `DOMINION_TLS_IPS`           | unset | Comma-separated extra IPs for the auto-issued server cert (e.g. droplet public IP). |
| `DOMINION_CA_CERT_PEM` / `DOMINION_CA_KEY_PEM` | unset | Persistent Ed25519 CA. When blank, a fresh CA is generated on boot (agents issued before a restart stop authenticating). |
| `DOMINION_ENCRYPTION_KEY`    | random per run | base64 AES-256 key for encrypting stored OAuth refresh tokens. Keep stable or rotate with re-consent. |
| `DOMINION_GRAPH_CLIENT_ID` / `_CLIENT_SECRET` | unset | Microsoft Graph OAuth app credentials. When blank, the polling loop and `/connectors/graph/*` are disabled. |
| `DOMINION_GRAPH_TENANT_ID`   | `common` | Entra tenant id or `common` for multi-tenant. |
| `DOMINION_GRAPH_REDIRECT_URL` | `http://localhost:3000/connectors/graph/callback` | Must match the Entra app registration. |
| `DOMINION_DEV_GRAPH_SIMULATE` | `false` | **Dev only.** Exposes `/admin/connectors/graph/simulate` for ingest tests without live M365. |
| `DOMINION_ANTHROPIC_API_KEY` | unset | Anthropic Messages API key. When blank, triage uses a deterministic stub draft. |
| `DOMINION_ANTHROPIC_MODEL`   | `claude-haiku-4-5-20251001` | Claude model used for triage. |
| `DOMINION_TRIAGE_INTERVAL`   | `5m` | Triage pass cadence. |
| `DOMINION_TRIAGE_LOOKBACK`   | `1h` | How far back each pass scans for untriaged emails. |
| `DOMINION_DEV_SIMULATE_SEND` | unset | **Dev only.** When `true`, `/me/queue/*/approve` records a fake `sentMessageId` instead of calling Graph. Auto-on when `DOMINION_DEV_GRAPH_SIMULATE=true` and no Graph client is configured. |
| `DOMINION_FGA_API_URL`       | unset | e.g. `http://openfga:8080`; when unset ACL is disabled |
| `DOMINION_FGA_STORE_NAME`    | `dominion` | OpenFGA store to bootstrap |
| `DOMINION_DEV_PRINCIPAL_HEADER` | `false` | **Dev only.** When `true`, trusts `X-Dominion-Dev-Principal: user:<uuid>` header. |

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
