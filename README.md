# Dominion (internal codename: Agentic Dominos)

Self-hostable governance spine for agentic AI in the enterprise: sovereign
document store, per-document ACLs, first-class AI identities, and a unified
audit log that a compliance officer can export with one call.

This repository is the **MVP (v0) — thinnest-slice build** as described in
`docs/architecture.md`. It exists to prove one claim: every AI action in this
system is attributable, scoped, auditable, and revocable.

## Status

Phase 0 — repo bootstrap. See `docs/architecture.md` §5 for the full phase plan.

## Repository layout

```
packages/
  api-gateway/      Go service. Single entry point for all client traffic.
                    Auth + ACL + audit middleware chain. Phase 0+.
  agent-runtime/    Fork of github.com/openclaw/openclaw with a governance
                    wrapper. Phase 5.
  email-worker/     Microsoft Graph polling worker. Phase 6.
  admin-ui/         Next.js single-page admin console. Phase 9.
infra/
  docker-compose.yml   Local Postgres + OpenFGA for dev.
docs/
  architecture.md      The north-star spec.
```

## Prerequisites

- Go 1.23+
- Docker 24+ with the `docker compose` subcommand
- Node 22+ and pnpm 10+ (only needed once the admin UI lands in Phase 9)

## Quick start (Phase 0)

```sh
make up          # start Postgres + OpenFGA in Docker
make gateway     # run the api-gateway on :3000
curl localhost:3000/health
# -> {"status":"ok"}
make down        # stop the docker services
```

## Key decisions (from the §7 escalations)

| Decision | Choice |
| --- | --- |
| Gateway language | Go |
| ACL engine | OpenFGA |
| Default LLM provider | Claude (Anthropic), BYOK supported |
| Source license | Deferred — repo is all-rights-reserved until chosen |
| OpenClaw UX in the fork | Strip; re-add only what the admin UI needs |

## Non-goals for the MVP

No web UI beyond login + approval queue. No MCP connector library (email
only). No matter workspaces, specialist-agent library, or citizen-developer
builder. No CRDT / laptop replication. Single-tenant. No SOC 2 / ISO 27001.
No billing. Everything in that list is v1+.
