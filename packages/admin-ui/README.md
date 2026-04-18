# admin-ui

Single-page Next.js admin console for Dominion (spec §5 phase 9).
Implements the six areas the spec calls out — Agents, Users, ACL
Grants, Audit Explorer, Approval Queue, and a Triage trigger — on
one page with tabs. Styling is plain Tailwind v4; we're not pulling
in a component library.

## What it does

- Stores the gateway URL, admin bearer token, and a dev-principal
  ("view-as") string in `localStorage`.
- Calls `/admin/*` routes with `Authorization: Bearer $ADMIN_TOKEN`.
- Calls `/me/*` routes with `X-Dominion-Dev-Principal: $PRINCIPAL`
  (until the UI is wired to the real OIDC session cookie — not in
  scope for the MVP).
- Does no server-side work of its own — every action hits the gateway
  directly. The gateway must set `DOMINION_CORS_ORIGIN` to the UI's
  origin for the browser to accept the responses.

## Hero-flow coverage (spec §4)

A non-engineer can run the full flow without touching curl:

1. **Agents** tab → _create agent_ "Astrid" with `owner_user_id` = the
   partner's user id (from the Users tab lookup).
2. **Triage** tab → _run now_. Stats show `drafts_created >= 1`.
3. **Queue** tab → set view-as = `user:<partner>` → _refresh_ →
   _approve &amp; send_.
4. **Audit** tab → filter `actor=agent:<astrid>` → verify the event
   stream.
5. **Agents** tab → _revoke_ Astrid. Every subsequent call as Astrid
   returns 401; the Audit tab shows the revocation event.

## Dev

```sh
cd packages/admin-ui
npm install
npm run dev              # http://localhost:3100
```

Make sure the gateway is running with:
```
DOMINION_CORS_ORIGIN=http://localhost:3100
```

## Prod (docker compose)

Compose builds and runs `admin-ui` on port 3100 when the stack is
started. Set `DOMINION_ADMIN_UI_ORIGIN` in `infra/.env` to match the
public URL (default `http://<droplet-ip>:3100`) and the gateway will
emit the right CORS headers.
