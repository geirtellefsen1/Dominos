# Dominion MVP — Architecture (phase tracker)

The full spec lives in the initiating task description / PR body. This file
is the short in-repo reference.

## Components (spec §3)

- API gateway (Go) — auth + ACL + audit middleware chain
- Document store — Postgres 16 + JSONB, schema registry
- ACL engine — OpenFGA, Zanzibar-style relationship tuples
- Identity service — Entra ID (humans) + X.509 certs (agents)
- Agent runtime — fork of github.com/openclaw/openclaw
- Email worker — Microsoft Graph polling
- Audit log — append-only, Ed25519-signed, partitioned by month

## Phases (spec §5)

| # | Name | Acceptance test | Status |
| - | ---- | --------------- | ------ |
| 0 | Repo bootstrap | `docker compose up` + gateway `/health` | complete |
| 1 | Document store + schema registry | create/read doc, reject malformed | complete |
| 2 | Identity + OIDC login | login, `/me`, SCIM deactivate kills sessions | complete |
| 3 | ACL engine | grant/deny works, audit records both | complete |
| 4 | Audit log | signed bundle export, tamper detected | complete |
| 5 | AI identity + OpenClaw fork | mTLS agent, gateway-routed tools | complete (OpenClaw surgery deferred) |
| 6 | Email ingestion | new mail → `email.v1` doc < 90s | **in progress** |
| 7 | Hero flow (Astrid + approval queue) | end-to-end §4 runs | pending |
| 8 | Offboarding primitive | one DELETE revokes everywhere | pending |
| 9 | Minimal admin UI | non-engineer can run §4 via UI | pending |

## The non-negotiable invariant

Every agent tool call routes through the gateway. No agent touches Postgres,
OpenFGA, or Graph directly. If a proposed change would relax this, escalate
before committing.
