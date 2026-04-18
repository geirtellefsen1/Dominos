# Dominion MVP — Architecture & Build Spec

**Project codename:** Agentic Dominos / Project Dominion
**Document type:** Technical architecture + build plan for Claude Code
**Status:** v0.1 — MVP (thinnest-slice) specification
**Audience:** Claude Code (primary), human co-founders / engineers (secondary)
**Upstream runtime to fork:** https://github.com/openclaw/openclaw

## 0. How to read this document

This is the north-star spec for the Dominion MVP. Claude Code should
treat every section below as authoritative. When in doubt, prefer the
architecture described here over anything Claude finds in the OpenClaw
upstream, because OpenClaw is a hacker's runtime and Dominion is the
enterprise governance shell around it.

The MVP is deliberately narrow. The Dominion sales brief and product
spec describe a full v1 product with matter workspaces, specialist
agent libraries, MCP connector suites, citizen-developer app builder,
laptop replication, and more. None of that is in the MVP. The MVP
exists to prove one claim: every AI action in this system is
attributable, scoped, auditable, and revocable. If the MVP does that
convincingly, everything else can be built on top.

## 1. What the MVP is (and isn't)

### 1.1 One-sentence definition

The Dominion MVP is a self-hostable server that stores documents with
per-document access control, authenticates humans through Entra ID,
ingests email into a sovereign store, runs OpenClaw-based AI agents
under scoped identities, and produces a unified audit log that a
compliance officer can export with one call.

### 1.2 What is in the MVP (v0)

- A document store (Postgres + JSONB) with a schema registry.
- A per-document ACL engine (OpenFGA or an equivalent
  relationship-based access control library).
- Human identity via Entra ID / SCIM (OIDC login, provisioning).
- AI identity as a first-class primitive — every agent has its own
  identity, certificate, and scoped permissions.
- An agent runtime forked from openclaw/openclaw, wrapped in a
  governance layer so every agent invocation passes through identity
  + ACL + audit.
- Email ingestion from Microsoft 365 (Graph API) into the document
  store, tagged by user/mailbox.
- A unified audit log — every read, write, agent invocation, ACL
  change, and identity event, with signed timestamps.
- A minimal admin API for: creating users and agents,
  granting/revoking access, exporting audit logs, and offboarding
  (the one-revoke primitive).
- A tiny hero flow: the user's personal assistant agent triages
  unread email and produces draft replies that land in an approval
  queue. This proves the end-to-end governance loop works.

### 1.3 What is explicitly NOT in the MVP

- No web UI beyond what's needed to log in and view the approval
  queue (a single-page admin console is acceptable).
- No MCP connector library — email ingestion only. Slack, Drive,
  Calendar, CRM all deferred.
- No matter workspaces, no specialist-agent library, no
  citizen-developer app builder.
- No CRDT / laptop replication / offline mode.
- No multi-tenancy. Single-tenant only.
- No SOC 2, no ISO 27001 — those come after the MVP proves the
  architecture.
- No billing, no marketing site, no customer onboarding flow.

Every one of these belongs in v1+. The MVP's job is to prove the
governance spine works.

## 2. Architectural thesis (why the pieces are shaped this way)

The uploaded thesis and product spec argue that Lotus Notes/Domino had
the architecturally correct enterprise model — sovereign store,
per-document ACLs, certificate identity, scheduled agents, one-revoke
offboarding — and that the AI era makes that model urgent again. The
MVP preserves those five primitives and modernizes them:

| Domino primitive (1989–2015)               | Dominion MVP equivalent                                        |
| ------------------------------------------ | -------------------------------------------------------------- |
| NSF document store with views              | Postgres + JSONB documents with a schema registry              |
| Per-document ACLs (readers/authors fields) | OpenFGA-style relationship-based ACL engine                    |
| X.509 Notes certificates                   | Entra ID for humans, X.509 certs for AI identities             |
| LotusScript / Formula scheduled agents     | OpenClaw-forked agent runtime with governance wrapper          |
| One-revoke offboarding via cert revocation | One API call revokes identity → cascades through ACLs, agents, sessions |

The AI identity concept is the thing Domino could not have had. In
Dominion, an AI agent is not a script running as a user — it is its
own identity, with its own permissions, its own audit trail, and its
own revocation. When the partner's personal assistant "Astrid" drafts
a reply, the draft is attributable to Astrid; when the partner
approves it, the send is attributable to the partner. This
distinction is the core of the trust claim and must be preserved
through every layer of the implementation.

## 3. System components

```
                 ┌─────────────────────────────────────────────┐
                 │             Admin / Approval UI             │
                 │       (minimal SPA — React/Next.js)         │
                 └──────────────────────┬──────────────────────┘
                                        │ HTTPS (OIDC session)
                 ┌──────────────────────▼──────────────────────┐
                 │              Dominion API Gateway           │
                 │   Auth middleware · ACL check · Audit tap   │
                 └──┬───────────┬────────────┬────────────┬────┘
                    │           │            │            │
         ┌──────────▼──┐   ┌────▼────┐  ┌────▼─────┐  ┌───▼──────────┐
         │  Document   │   │  ACL    │  │  Agent   │  │  Audit Log   │
         │  Store      │   │  Engine │  │  Runtime │  │  (append-    │
         │ (Postgres + │   │(OpenFGA)│  │ (OpenClaw│  │   only,      │
         │   JSONB)    │   │         │  │   fork)  │  │   signed)    │
         └──────┬──────┘   └─────────┘  └────┬─────┘  └──────────────┘
                │                            │
         ┌──────▼──────┐              ┌──────▼──────┐
         │ Email       │              │ Identity    │
         │ Ingestion   │              │ Service     │
         │ Worker      │              │ (Entra ID + │
         │ (MS Graph)  │              │ AI certs)   │
         └─────────────┘              └─────────────┘
```

### 3.1 Dominion API Gateway

The single entry point for all client traffic. Every request flows
through three middlewares, in order:

1. **Auth middleware.** Validates the OIDC token (for humans) or mTLS
   client cert (for AI identities). Produces a Principal object
   attached to the request context.
2. **ACL check.** Every API route declares the resource type it
   touches and the action it performs. Middleware calls OpenFGA:
   "can Principal do action on resource?" No route bypasses this.
3. **Audit tap.** Every request — allowed or denied — is logged to
   the audit service with full request/response metadata.

Implementation: a single Node.js/TypeScript service (Fastify or
Express), or Go, behind a reverse proxy. Stateless; horizontally
scalable later.

### 3.2 Document Store

Postgres 16 with JSONB columns. Every document is a row in a
`documents` table:

```sql
CREATE TABLE documents (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  schema_id       TEXT NOT NULL REFERENCES schemas(id),
  tenant_id       UUID NOT NULL,            -- reserved for v2 multi-tenancy
  body            JSONB NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_by      TEXT NOT NULL,            -- principal id (user or agent)
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by      TEXT NOT NULL,
  deleted_at      TIMESTAMPTZ               -- soft delete only
);

CREATE TABLE schemas (
  id              TEXT PRIMARY KEY,         -- e.g. "email.v1", "note.v1"
  json_schema     JSONB NOT NULL,           -- validation schema
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON documents (schema_id);
CREATE INDEX ON documents USING GIN (body);
```

MVP ships with two schemas: `email.v1` (ingested mail) and `draft.v1`
(agent-produced email drafts awaiting approval). More schemas are
added by `POST /admin/schemas` — but the MVP does not try to build a
UI for this.

### 3.3 ACL Engine

Relationship-based access control modeled on Google's Zanzibar. Use
OpenFGA (https://openfga.dev) as the implementation. The MVP model:

```
type user
type agent
type document
  relations
    define owner: [user, agent]
    define reader: [user, agent] or owner
    define writer: [user, agent] or owner
```

Every document creation calls OpenFGA to write a tuple:
`document:<id>#owner@user:<id>`. Every API call that reads a document
calls `check(user:<id>, reader, document:<id>)`. If the check returns
false, the API returns 403 and the audit tap records a `access.denied`
event.

Agent identities get their own tuples. When the partner's PA is
created, it gets its own user-like principal and the firm admin
grants it `reader` on a narrow set of documents. No agent inherits
its user's permissions automatically — agent permissions are explicit,
always.

### 3.4 Identity Service

Two kinds of identity, one model:

- **Human identity:** Entra ID via OIDC. Login flow mints a session
  JWT. SCIM endpoint accepts provisioning calls from the customer's
  Entra tenant so users appear/disappear in Dominion as Entra
  assigns/removes them.
- **AI identity:** issued by a private CA run by Dominion. When the
  admin creates an agent, Dominion generates a keypair, signs the
  public key with the firm's internal CA, and stores the cert
  thumbprint as the agent's principal id. The agent authenticates to
  the API with mTLS, presenting its cert.

**The one-revoke primitive:** `DELETE /admin/identities/<id>` does
three things atomically.

1. Marks the identity as revoked in the identity table.
2. Fires a CRL update — any cached cert immediately fails
   verification.
3. Calls OpenFGA to remove every tuple where this identity is a
   subject.

After revocation, no new session can be created, no existing session
can perform any action (ACL returns false on every resource), and any
agent running under that identity terminates on its next heartbeat.
Humans and AI agents use the same primitive.

### 3.5 Agent Runtime (OpenClaw fork)

Fork https://github.com/openclaw/openclaw into a new repo
`dominion/agent-runtime`. Keep OpenClaw's runtime loop, task queue,
and LLM adapter layer. Wrap everything else.

Mandatory changes on top of the fork:

- **Every agent invocation must present an AI identity cert.** Strip
  any path in OpenClaw where an agent runs anonymously or with a
  shared service account. If Claude Code finds such a path, it
  removes it and routes through the Identity Service.
- **Every tool call an agent makes must pass through the Dominion API
  Gateway.** Agents do not call Postgres, OpenFGA, or Graph API
  directly. They call `https://<dominion>/api/...` and are subject to
  the same auth + ACL + audit middleware as humans. This is the
  single most important change; the governance claim collapses if an
  agent can bypass the gateway.
- **Every agent invocation logs to the audit service.** Start, stop,
  LLM tokens used, tool calls attempted, tool calls allowed, tool
  calls denied.
- **No autonomous external action in v0.** Agents propose; humans
  approve. In the MVP, the `send_email` tool does not exist — only
  `draft_email`, which writes a `draft.v1` document to the store. A
  human must explicitly approve the draft before any outbound email
  leaves.

Why fork, not use upstream as a library: the governance changes above
are invasive. They touch OpenClaw's hot paths. A fork is cleaner than
a plugin.

Upstream sync policy: merge from openclaw/openclaw main no more than
quarterly, and only after running the Dominion compliance test suite
against the merged code.

### 3.6 Email Ingestion Worker

A background worker (same language as the API gateway — keeps
dependencies down) that:

- Holds an OAuth refresh token per user (obtained when the user
  consents through the admin UI).
- Polls Microsoft Graph `/me/messages` on a schedule (every 60
  seconds for the MVP — webhooks later).
- For each new message: creates an `email.v1` document with the
  message body and metadata, owned by the user, with the user's PA
  granted `reader`.

Out of scope for v0: outbound send (drafts only), Gmail/Proton
ingestion, webhooks, threading/folders.

### 3.7 Audit Log

Append-only. Every entry is a signed record:

```json
{
  "id": "01JABC...",
  "timestamp": "2026-04-17T09:15:22.193Z",
  "actor": "agent:ast_7h3k...",
  "on_behalf_of": "user:partner.smith@firm.com",
  "action": "document.read",
  "resource": "document:e4f8...",
  "decision": "allow",
  "context": { "ip": "...", "request_id": "..." },
  "signature": "ed25519:..."
}
```

Storage: Postgres table, partitioned by month, with an INSERT-only
role for the API gateway. Each entry is signed with the Dominion
server's Ed25519 key so tampering is detectable on export.

Export: `GET /admin/audit?matter=<id>&from=<ts>&to=<ts>` returns a
signed JSON bundle. For the MVP, "matter" scoping is a stretch goal —
baseline is from/to plus optional actor filter.

### 3.8 Admin API (the minimum surface)

Every route requires an `admin` role on the caller. All routes return
JSON.

```
POST   /admin/users                       # create human (usually via SCIM)
DELETE /admin/users/:id                   # one-revoke (see Identity Service)
POST   /admin/agents                      # create AI identity + assign scope
DELETE /admin/agents/:id                  # one-revoke for agent

POST   /admin/acl/grant                   # { principal, relation, resource }
POST   /admin/acl/revoke

GET    /admin/audit                       # export audit bundle (signed JSON)

POST   /admin/schemas                     # register new document schema
GET    /admin/schemas
```

And the hero-flow user-facing routes:

```
GET    /me                                # who am I, what's my PA
GET    /me/inbox                          # recent email documents
GET    /me/queue                          # drafts awaiting my approval
POST   /me/queue/:id/approve              # approve + send via Graph
POST   /me/queue/:id/reject
```

## 4. The hero flow (what "done" looks like for the MVP)

A tester can:

1. Log in with Entra ID.
2. Admin creates an agent named "Astrid" for the tester, gives Astrid
   `reader` on the tester's inbox documents.
3. The tester receives emails in their real M365 mailbox.
4. Within 60 seconds those emails appear as `email.v1` documents in
   Dominion.
5. Astrid triages them on a schedule (every 5 minutes), produces
   `draft.v1` reply drafts for routine ones.
6. The tester sees drafts in `/me/queue`, approves one.
7. Dominion sends the approved reply from the tester's mailbox via
   Graph API.
8. The compliance officer calls
   `GET /admin/audit?actor=agent:astrid&from=...` and receives a
   signed JSON file listing every read, every tool call, every
   decision Astrid made.
9. The admin calls `DELETE /admin/agents/astrid` and from that moment
   Astrid cannot read anything, cannot run, and her sessions are
   terminated.

If steps 1–9 all work, the MVP is done. Everything else — matter
workspaces, specialist agents, MCP connectors, UI polish — is v1
territory.

## 5. Build phases (for Claude Code)

Each phase ships end-to-end value and has a concrete acceptance test.
Claude Code should complete one phase fully, verify the acceptance
test, then move to the next. No skipping ahead.

### Phase 0 — Repo bootstrap (½ day)

- Create a monorepo `dominion/` with these packages:
  - `packages/api-gateway` (TypeScript / Fastify, or Go — Claude Code picks)
  - `packages/agent-runtime` (fork of openclaw/openclaw, see §3.5)
  - `packages/email-worker`
  - `packages/admin-ui` (Next.js, minimal)
  - `infra/` (docker-compose for local Postgres + OpenFGA)
- Top-level `README.md`, `CLAUDE.md`, `.editorconfig`, `.gitignore`,
  license (BSL or ELv2 to match the product plan).
- **Acceptance:** `docker-compose up` starts Postgres and OpenFGA;
  `pnpm dev` (or equivalent) boots the API gateway on `:3000` and it
  responds `{"status":"ok"}` on `GET /health`.

### Phase 1 — Document store + schema registry (1 day)

- Postgres migrations for the tables in §3.2.
- Seed the two MVP schemas: `email.v1`, `draft.v1`.
- API routes: `POST /documents`, `GET /documents/:id`,
  `GET /documents?schema=...` (paginated). Soft-delete only.
- Schema validation on write using Ajv (or equivalent).
- **Acceptance:** create a document, read it back, reject a malformed
  document with a 400 containing the schema error.

### Phase 2 — Identity + OIDC login (1 day)

- Entra ID OIDC integration. Dev mode uses a local OIDC mock (e.g.
  `mock-oauth2-server`) so testing doesn't require a real Entra
  tenant.
- Session JWTs, refresh flow, logout.
- SCIM v2 endpoint for Users (create, update, deactivate).
- **Acceptance:** log in through the mock IdP, get a session JWT,
  call `GET /me`, receive your profile. Deactivating the user via
  SCIM kills their sessions within 60 seconds.

### Phase 3 — ACL engine (1–2 days)

- Bring up OpenFGA locally. Load the model from §3.3.
- Middleware that pulls Principal off the request, looks up the
  route's (action, resource) declaration, calls OpenFGA check.
- `POST /admin/acl/grant` and `/revoke` routes writing tuples.
- **Acceptance:** user A creates a document. User B calls
  `GET /documents/:id` and gets 403. Admin grants B `reader`. B's
  next call succeeds. Audit log shows the denied attempt and the
  subsequent allow.

### Phase 4 — Audit log (1 day)

- Audit table with monthly partitions.
- Middleware hook at the end of every request writes the entry.
- Ed25519 signing key loaded from env (generate one for dev).
- `GET /admin/audit` returns signed JSON bundle.
- **Acceptance:** run 100 requests, export the bundle, verify every
  signature with the public key. Tamper with one byte; signature
  verification fails on that entry only.

### Phase 5 — AI identity + OpenClaw fork wired in (2–3 days)

- Fork https://github.com/openclaw/openclaw into
  `packages/agent-runtime`. Strip anonymous / shared-account code
  paths per §3.5.
- `POST /admin/agents` creates an AI identity: generate keypair, sign
  cert with internal CA, store cert thumbprint as principal id,
  persist private key in a secrets store (dev: encrypted file; prod:
  Vault/KMS).
- Agent processes authenticate to the API gateway via mTLS using
  their issued cert.
- First tool available to agents: `dominion.documents.read` and
  `dominion.documents.write` — both routed through the gateway, both
  subject to the same ACL and audit.
- **Acceptance:** spawn an agent process, have it read a document it
  has access to, attempt to read one it doesn't (gets 403), write a
  draft document. All three appear in the audit log as
  `actor=agent:<id>`.

### Phase 6 — Email ingestion (1–2 days)

- OAuth consent flow for Microsoft Graph.
- Polling worker as in §3.6 — one Graph poll per user every 60
  seconds.
- On new message: `POST /documents` with schema `email.v1`, owner =
  user, reader = user's PA.
- **Acceptance:** send a test email to the connected mailbox; within
  90 seconds it appears in `GET /me/inbox` and as a document in the
  store.

### Phase 7 — Hero flow: Astrid triages + approval queue (2–3 days)

- A scheduled agent task that, every 5 minutes, reads the user's last
  hour of unread `email.v1` documents and produces `draft.v1` reply
  drafts for routine ones (use the LLM's own judgement via a simple
  prompt; this is fine for the MVP).
- `GET /me/queue`, `POST /me/queue/:id/approve`,
  `POST /me/queue/:id/reject`.
- On approve: call Graph `sendMail` as the user, mark the draft as
  sent.
- **Acceptance:** the full §4 hero flow runs end-to-end against a
  real M365 tenant.

### Phase 8 — Offboarding primitive (½ day)

- `DELETE /admin/users/:id` and `DELETE /admin/agents/:id` implement
  the three-step revoke in §3.4.
- **Acceptance:** revoke Astrid. She immediately cannot read any
  document, her next scheduled run exits with 401, and the audit log
  shows the revocation event plus all her subsequent denied attempts.

### Phase 9 — Minimal admin UI (2 days)

- Next.js SPA, single user role = admin.
- Pages: Users, Agents, ACL Grants, Audit Explorer, Approval Queue.
- No design system needed — shadcn/ui defaults are fine.
- **Acceptance:** a non-engineer admin can do the entire §4 hero flow
  through the UI without touching curl.

**Total MVP estimate:** ~12–16 engineer-days of focused work, assuming
Claude Code does the heavy lifting and a human reviews at each phase
boundary.

## 6. Non-functional requirements (MVP only)

- **Deployment:** single-box Docker Compose is enough for the MVP.
  Kubernetes / managed Postgres come later.
- **Region:** all infrastructure hosted in the EU. For dev, any
  region is fine; for design-partner demos, Frankfurt or Stockholm.
- **Secrets:** Env vars for dev. Vault/KMS for any shared demo
  deployment.
- **Observability:** structured JSON logs + Prometheus metrics on
  the gateway. No full APM in the MVP.
- **Testing:** unit tests per package; one end-to-end test per
  phase's acceptance criterion. CI via GitHub Actions.
- **Source license:** BSL 1.1 (converts to Apache 2.0 after 3 years)
  or Elastic License v2 — match the sales brief's "source-available"
  positioning. Claude Code should confirm with a human before
  committing the license file.

## 7. Open decisions Claude Code should escalate, not guess

These are judgement calls that affect product positioning. Claude
Code should surface them to the human reviewer rather than pick
unilaterally.

1. **Language for the gateway.** TypeScript (ecosystem, hiring) vs
   Go (performance, binary deployment). The MVP works either way;
   the choice is cultural.
2. **Initial LLM provider.** Claude (Anthropic) vs Azure OpenAI vs
   bring-your-own-key default. Sales brief says BYOK with a platform
   default; MVP default model choice is still open.
3. **OpenFGA vs SpiceDB.** Both implement Zanzibar-style ACL.
   OpenFGA has better TypeScript support; SpiceDB has better
   performance. Either is fine for the MVP.
4. **Source license exact choice.** BSL vs ELv2. Both satisfy
   "source-available, not OSS." Legal should pick.
5. **How much of OpenClaw's upstream UX to keep in the fork.** If
   OpenClaw ships with a CLI and a dashboard, do we keep them as dev
   tools or strip them? Default: strip; re-add only what the
   Dominion admin UI needs.

## 8. What Claude Code should do first

1. Read this entire document before writing any code.
2. Clone https://github.com/openclaw/openclaw and skim its README
   and top-level structure. Produce a one-page summary of what's
   inside so the human reviewer can sanity-check before the fork.
3. Ask the human reviewer the five escalation questions in §7.
4. Start Phase 0.

Do not start Phase 1 until Phase 0's acceptance test passes. Do not
merge upstream OpenClaw into the fork without running the compliance
test suite (which, in the MVP, is Phase 3's ACL tests + Phase 4's
audit tests).

## 9. Glossary

- **Principal** — any authenticated actor. Either a `user:<id>` or
  `agent:<id>`.
- **Identity** — the persistent record of a principal, including
  credentials (cert or SCIM-provisioned user) and revocation status.
- **ACL tuple** — a row in the OpenFGA store, of the form
  `(resource, relation, principal)`.
- **Matter** — a client engagement workspace. Post-MVP concept;
  listed here because it appears throughout the source documents.
- **PA** — personal assistant. The named AI agent that belongs to a
  user.
- **One-revoke** — the architectural property that a single admin
  action removes a principal's access from every document, agent,
  and session in the system atomically.
- **OpenClaw** — the open-source agent runtime at
  https://github.com/openclaw/openclaw that Dominion forks.
- **Dominion** — the product. "Agentic Dominos" is the working
  internal name; "Dominion" is the external product name used in
  the sales brief.
