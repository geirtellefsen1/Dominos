# Guidance for Claude Code in this repository

## Read this first

The north-star spec is `docs/architecture.md`. It is authoritative. When it
disagrees with OpenClaw upstream or with Claude's prior assumptions, the spec
wins — OpenClaw is a hacker's runtime and Dominion is the enterprise
governance shell around it.

## Build phases (from spec §5)

Work one phase at a time. Do **not** start phase N+1 until phase N's
acceptance test passes.

- [x] Phase 0 — Repo bootstrap
- [x] Phase 1 — Document store + schema registry (pending live DB validation)
- [x] Phase 2 — Identity + OIDC login (pending live IdP + DB validation)
- [x] Phase 3 — ACL engine (OpenFGA)
- [x] Phase 4 — Audit log (signed, append-only)
- [x] Phase 5 — AI identity + minimal agent CLI (OpenClaw fork deferred)
- [x] Phase 6 — Email ingestion (Microsoft Graph)
- [ ] Phase 7 — Hero flow: Astrid triages + approval queue
- [ ] Phase 8 — Offboarding primitive (one-revoke)
- [ ] Phase 9 — Minimal admin UI

## Locked decisions (do not re-open without human approval)

- **Gateway language:** Go
- **ACL engine:** OpenFGA
- **Default LLM provider:** Claude (Anthropic); BYOK supported
- **Source license:** deferred — do not add a LICENSE file until legal picks
- **OpenClaw upstream UX:** strip by default; re-add only what the admin UI
  needs

## The one rule that cannot be compromised

Every agent tool call must pass through the Dominion API gateway. Agents
never call Postgres, OpenFGA, or Graph directly. The governance claim
collapses if an agent can bypass the gateway.

## Escalate, don't guess

If a change would relax the governance model (e.g. a fast path that skips
auth, ACL, or audit), stop and ask a human reviewer before committing.

## Development branch

All MVP work happens on `claude/dominion-mvp-architecture-bNsDx` until
otherwise specified.
