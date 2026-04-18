# Dominion MVP — demo manual

A 5-minute live demo script. Proves the north-star claim in one sitting:

> every AI action in this system is attributable, scoped, auditable,
> and revocable.

---

## 0. Before the demo (one-time, on the droplet)

SSH to `Dominos-Server-01` once:

```sh
cd /opt/dominion
# 1. Make sure the stack is up and healthy
docker compose --env-file infra/.env -f infra/docker-compose.yml ps

# 2. Read the tokens you'll paste into the UI
grep -E '^(DOMINION_ADMIN_TOKEN|DOMINION_SCIM_TOKEN)=' infra/.env

# 3. Confirm CORS is set for the phone's browser
grep '^DOMINION_CORS_ORIGIN=' infra/.env
# → DOMINION_CORS_ORIGIN=http://139.59.173.17:3100

# 4. Run the smoke test once to seed alice, bob, and Astrid + sample email
DOMINION_PUBLIC_URL=http://localhost:3000 \
DOMINION_ENV_FILE=infra/.env \
  scripts/smoke-test.sh | tee /tmp/smoke.log
```

When the smoke test finishes, pull the three ids you'll use on stage:

```sh
grep -E 'alice=|bob=|agent [0-9a-f]{8}' /tmp/smoke.log
# P. provisioning alice + bob via SCIM
#   ok   alice=<ALICE_UUID> bob=<BOB_UUID>
# 5a. POST /admin/agents (create Astrid)
#   ok   agent <ASTRID_UUID> created (thumbprint ...)
```

Write those three UUIDs on a sticky note. You'll type them into the UI.

---

## 1. Open the UI on the demo device

Navigate the browser to:

```
http://139.59.173.17:3100
```

The Dominion Admin page loads. At the top:

| Field                | Value                                 |
| -------------------- | ------------------------------------- |
| Gateway URL          | `http://139.59.173.17:3000`           |
| Admin bearer token   | paste the `DOMINION_ADMIN_TOKEN`      |
| View-as principal    | leave blank for now                   |

Tap **save**.

---

## 2. The demo flow

Six screens. Each one has a single sentence to say out loud.

### Screen 1 — Agents · _"This is an AI identity."_

Tap **agents** tab.

Show: the "Create AI agent" card. Form is pre-filled with `Astrid`.

Say:

> "In Dominion every AI agent has its own cryptographic identity,
> issued by our internal CA — same model Lotus Notes used in 1989,
> applied to AI. Astrid already exists from our bootstrap; if I wanted
> a new one, I'd type a name and click create, and the gateway would
> hand me back a fresh cert and private key, one time only."

### Screen 2 — Triage · _"The agent proposed drafts — it cannot send."_

Tap **triage** tab. Tap **run now**.

Show: the JSON result —
`{"pairs_checked":1,"emails_processed":1,"drafts_created":1,"emails_skipped":0,"errors":0}`

Say:

> "The triage engine ran every active PA against recent mail. Astrid
> saw one email, produced one draft. Note the response: **drafts
> created**, not messages sent. Phase 5's fork of the agent runtime
> strips every outbound tool; the only thing Astrid can do is propose."

### Screen 3 — Queue · _"The human — not the agent — approves the send."_

Tap **queue** tab. Paste `user:<ALICE_UUID>` into **view-as principal**,
tap **save**, then on the queue tab tap **refresh**.

Show: one draft card appears with the subject, recipient, and body.

Tap **approve & send**.

Show: the draft card updates, status is `sent`.

Say:

> "I'm now logged in as Alice, the partner. I see Astrid's draft, I
> read it, I approve. The send was attributed to me. Astrid could have
> written this a thousand times overnight — none of them would have
> left the building without this click."

### Screen 4 — Audit · _"Every step was logged, signed, and exportable."_

Tap **audit** tab. Put `agent:<ASTRID_UUID>` into the **actor** field,
leave `from` at its default (5 minutes ago), tap **export**.

Show: a table with rows — `document.read` by Astrid, the draft
creation, etc. Decision column shows `allow`.

Say:

> "Every row is Ed25519-signed with a server key whose public half we
> publish. A compliance officer exports this bundle and verifies every
> signature offline — we wrote the verifier in 20 lines of Python.
> Tamper with any byte and exactly that row fails verification."

### Screen 5 — Agents (revoke) · _"One button turns Astrid off."_

Tap **agents** tab again. In the "Revoke agent (one-revoke)" card,
paste `<ASTRID_UUID>`, tap **revoke**.

Show: the result JSON —
`{"agent":{..., "active":false, ...}, "tuples_removed": N}`

Say:

> "One call, three effects atomically: the identity row flips to
> inactive, her certificate stops authenticating on the next request,
> and every ACL tuple where Astrid was a subject is deleted. Cert
> revocation lists in the Notes days; in Dominion it's a DELETE."

### Screen 6 — Audit (after revoke) · _"The revocation itself is on the log."_

Back to the **audit** tab. Tap **export** again (same filter).

Show: a new row near the top — action `agent.revoke`, resource
`agent:<ASTRID_UUID>`, decision `allow`, actor `anonymous` (the admin
call).

Say:

> "And the revocation itself is in the audit log. Attributable,
> scoped, auditable, revocable — those four together are what Notes
> got right in 1989 and what the MCP era has been missing. That's the
> MVP."

---

## 3. If something goes wrong mid-demo

| Symptom                                                | Recovery                                                                                                              |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| "admin token not set" error on any request             | you tapped save but the field was empty; re-paste and save again                                                      |
| CORS error in browser console                          | CORS allowlist doesn't include your origin; SSH in, set `DOMINION_CORS_ORIGIN` in `infra/.env`, re-run `deploy.sh`     |
| Queue is empty after triage                            | the seeded email pre-dates the triage lookback window; on the server, re-run `scripts/smoke-test.sh` to ingest a fresh one |
| Approve says "send_failed"                             | `DOMINION_DEV_SIMULATE_SEND` is off and no real Graph client is wired; edit `infra/.env` to `DOMINION_DEV_SIMULATE_SEND=true` and redeploy |
| After revoke, the demo device can still "see" Astrid   | stale response cache — pull-to-refresh the page                                                                       |
| Screen is completely blank                             | the UI container is down; `docker compose logs admin-ui \| tail -40` on the server                                    |

---

## 4. One-click reset between demos

If you're giving the demo back-to-back and want a clean audit log +
fresh drafts each time:

```sh
ssh dominos-server
cd /opt/dominion
docker compose --env-file infra/.env -f infra/docker-compose.yml down -v
scripts/deploy.sh --with-mock-oidc
# then re-run the bootstrap smoke test:
DOMINION_PUBLIC_URL=http://localhost:3000 DOMINION_ENV_FILE=infra/.env \
  scripts/smoke-test.sh
```

Total reset time: under 60 seconds.

---

## 5. Questions you'll get

**Q: Where's the LLM?**
A: Astrid calls Claude (`claude-haiku-4-5-20251001` by default) through
   the gateway's `/documents` tools. In this demo we're running the
   deterministic stub so the draft is predictable; flip
   `DOMINION_ANTHROPIC_API_KEY` in `.env` and you get real model output.

**Q: How is this different from MCP?**
A: MCP standardises _how_ tools are called. Dominion governs _whether_
   they should be. Every MCP tool call would route through this
   gateway: same auth, same ACL, same audit. The two are layered, not
   competing.

**Q: What about the CA rotating?**
A: In this demo we use an ephemeral CA; in a real deployment you set
   `DOMINION_CA_CERT_PEM` and `_KEY_PEM` to a persistent CA (HSM or
   Vault-backed) and rotate on your own schedule. Agent certs are
   1-year leaves; the CA is 10-year.

**Q: Can the agent just skip the gateway?**
A: Not without a client cert the gateway's CA signed. And that cert's
   thumbprint is checked against the `agents` table on every request,
   so a revoked agent's cached connection dies on its next call.
