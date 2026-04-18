# Dominion MVP — demo manual

An 8-minute live demo against `Dominos-Server-01`. The point of the
demo is to prove the four-word thesis in one sitting:

> every AI action is **attributable**, **scoped**, **auditable**, and
> **revocable**.

Each of those words gets its own screen. Sprint 1 + 2 land the
hardening that makes the claim defensible beyond a happy path, so
several screens also show what happens when someone tries to break
the invariant and the gateway says no.

---

## 0. Before the demo (one-time, on the droplet)

```sh
ssh dominos-server
cd /opt/dominion

# 1. Stack healthy
docker compose --env-file infra/.env -f infra/docker-compose.yml ps
# expect: postgres, openfga, mock-oidc, gateway, admin-ui — all up

# 2. Tokens
grep -E '^(DOMINION_ADMIN_TOKEN|DOMINION_SCIM_TOKEN)=' infra/.env

# 3. Seed alice, bob, a simulated email, and Astrid
DOMINION_PUBLIC_URL=http://localhost:3000 \
DOMINION_ENV_FILE=infra/.env \
    scripts/smoke-test.sh | tee /tmp/smoke.log

# 4. Pull the three ids you'll type into the UI
grep -E 'alice=|bob=|agent [0-9a-f]{8}' /tmp/smoke.log
```

Write the three UUIDs on a sticky note:

```
ALICE  = <uuid>
BOB    = <uuid>   (for the bob-can't-see-it scope demo)
ASTRID = <uuid>   (the PA)
```

Open the admin UI on the demo device:

```
http://139.59.173.17:3100
```

Paste into the top-of-page fields:
- **Gateway URL** = `http://139.59.173.17:3000`
- **Admin bearer token** = the `DOMINION_ADMIN_TOKEN` from step 2
- Leave **View-as principal** empty for now

Tap **save**.

---

## 1. The demo — 8 screens

Every screen has **one line to say**. The narrative arc mirrors the
four governance claims.

### Screen 1 — Agents tab · _Attributable (identity)_

What to do: the create form is pre-filled with `Astrid`. Don't touch
it. Say the line.

> "Every AI agent in Dominion has its own cryptographic identity,
> issued by our internal CA — same model Lotus Notes used in 1989,
> applied to AI. Astrid already exists; if I wanted a new one, I'd
> fill in the display name and tap create. The gateway would hand
> back a cert plus a private key, one-time reveal — we never store
> the private key again."

(Optionally demonstrate create for a throwaway agent; scroll down
and show the private_key_pem in the result card, then revoke it.)

### Screen 2 — Triage tab · _Agent proposes (cannot send)_

Tap **run triage now**.

The result JSON lands:
```json
{
  "pairs_checked":   1,
  "emails_processed": 1,
  "drafts_created":   1,
  "emails_skipped":   0,
  "errors":           0
}
```

Say:

> "I just asked the triage engine to scan Astrid's view of Alice's
> mailbox and draft replies. One email processed, one draft created.
> Note: **drafts** created, not messages sent. The agent runtime is
> forked from OpenClaw with every outbound tool stripped — Astrid
> can propose, nothing else."

### Screen 3 — Queue tab · _Scoped (only the right human sees it)_

Paste `user:<ALICE>` into **View-as principal** at the top, tap
**save**, then go to the queue tab and tap **refresh**.

One draft card appears with the subject, recipients, and body.

Switch view-as to `user:<BOB>`, tap **save**, tap **refresh** again.
The queue is empty.

Say:

> "Alice sees Astrid's draft. Bob — not granted — sees nothing.
> Same URL, same session, different principal: the ACL engine
> returns a different answer. If you're in the same firm but not on
> this matter, the document literally does not exist for you."

Switch view-as back to `user:<ALICE>`, tap **save**, **refresh**,
find the draft.

### Screen 4 — Queue → approve · _The human fires the send_

Tap **approve · send via Microsoft Graph**.

The draft card updates. The `body.status` flips to `sent` and —
this is the Sprint 1 #5 fix — `body.sentMessageId` is a real
RFC 5322 Message-ID like `<AM8PR...@PR12345.outlook.office365.com>`
(or `sim:<timestamp>` on the droplet's simulate-send mode).

Tap **approve** again on the same card. The UI shows a **409** with:

```
[409 not_pending] draft is not pending — already sent, rejected, or in flight
```

Say:

> "One click, one send. Double-click, one send — the gateway rejects
> the second approve because the draft is already committed. This
> isn't a UI lock-out, it's a CAS state machine in the DB: even if
> you refresh the page and hit approve from two tabs simultaneously,
> the mail goes out exactly once."

### Screen 5 — Audit tab · _Auditable (signed, exportable)_

Paste `agent:<ASTRID>` into the actor filter, leave from at the
default 5-minutes-ago, tap **export · signed JSON bundle**.

Table appears. Point out:

- `triage.draft_created` rows with actor = `agent:<ASTRID>`,
  `on_behalf_of` = `user:<ALICE>`. **Sprint 1 #3** — the scheduler
  emits these, not just the HTTP path.
- `graph.ingest` rows with actor = `system:graph-ingest` and
  on_behalf_of = the user.
- The `/me/queue/.../approve` row you just fired, with actor =
  `user:<ALICE>` and decision = allow.

Say:

> "Every row is Ed25519-signed with a key whose public half is in
> this bundle. A compliance officer verifies every signature offline
> — we wrote the verifier in 20 lines of Python. The scheduled work
> is on the log too: Astrid drafting overnight while no human's
> watching shows up here exactly like it would if she'd been
> driven by a button press."

Change the actor filter to `admin:root`, tap export again.

> "And every admin decision I made today — create agent, grant ACL,
> trigger triage, export this bundle — is attributed to the admin
> bearer's synthetic principal. Sprint 1 #2: we can tell _which_
> admin bearer fired what, even before we ship the real admin login
> flow."

### Screen 6 — Agents → revoke Astrid · _Revocable (one-way, atomic)_

Back to the **Agents** tab. Paste `<ASTRID>` into the revoke box.
Tap **revoke · one-way**.

Result card:

```json
{
  "agent":           { ..., "active": false, "revoked_at": "..." },
  "tuples_removed":  3
}
```

Say:

> "One call, three atomic effects: her identity row flips to
> inactive, her cert stops authenticating on the next request
> (Sprint 2 #11 paginates this so it works past 1000 tuples), and
> every ACL grant she ever had is deleted from the FGA graph. If
> Astrid were mid-flight on a draft right now, her next tool call
> would 401. That's the Domino one-revoke primitive — in the Notes
> days it was cert revocation lists; in Dominion it's a DELETE."

### Screen 7 — Users → revoke Alice · _Revocable cascades to her agents_

Switch to the **Users** tab. In the look-up card type Alice's email
(`grep` it off the droplet if you don't have it memorised) and tap
**find**. Copy her uuid into the revoke box. Tap **revoke · one-way**.

Result card:

```json
{
  "user":             { ..., "active": false },
  "sessions_revoked": 0,
  "tuples_removed":   N,
  "cascaded_agents":  [
    { "agent_id": "<ASTRID>", "tuples_removed": 0 }
  ]
}
```

Say:

> "Sprint 2 #10 closed a real gap: offboarding Alice used to leave
> Astrid running with full access to the mailbox we just cut Alice
> off from. Now one DELETE on the user cascades to every agent she
> owns. The `cascaded_agents` array in the response is the paper
> trail."

(In this flow Astrid was already revoked at Screen 6, so
`tuples_removed: 0` on the cascade — the point to make is that the
_array is there and non-empty_.)

### Screen 8 — Audit again · _The revocation itself is on the log_

Audit tab. Clear the actor filter. Tap **export**.

Scan the table for:
- `admin:root` fired `DELETE /admin/agents/<ASTRID>`.
- `admin:root` fired `DELETE /admin/users/<ALICE>`.
- Every subsequent attempt by Astrid or Alice returns `deny`.

Say:

> "And the revocation itself is in the audit log. The same
> mechanism that recorded Astrid proposing the draft recorded the
> admin revoking her. Attributable, scoped, auditable, revocable —
> four words, one architecture. That's Dominion's MVP."

---

## 2. If something goes wrong mid-demo

| Symptom                                                  | Recovery |
| -------------------------------------------------------- | -------- |
| "admin token not set" error on any request               | you tapped save but the field was empty; re-paste and save again |
| Queue is empty after triage                              | the seeded email pre-dates the lookback window; on the server, re-run `scripts/smoke-test.sh` to ingest a fresh one |
| Approve returns `send_failed`                            | real Graph token not wired; set `DOMINION_DEV_SIMULATE_SEND=true` in `infra/.env` and redeploy for fake-send mode |
| `9becad9`/paginate revoke appears to remove 0 tuples     | Astrid was already revoked by a previous demo; re-seed via the smoke test |
| Page is stuck showing the old state after revoke         | pull-to-refresh the browser; the UI caches the previous bundle export until reopened |
| Audit tab shows `actor=anonymous` rows                   | those are from before the Sprint 1 #2 deploy — bundle older than the upgrade |
| `/admin/audit` returns `skipped_other_keys=N`            | same — rows signed by an earlier ephemeral audit key; filter by `from=<time after redeploy>` to hide them |

---

## 3. Back-to-back reset (under 60 seconds)

```sh
ssh dominos-server
cd /opt/dominion
docker compose --env-file infra/.env -f infra/docker-compose.yml down -v
scripts/deploy.sh --with-mock-oidc
DOMINION_PUBLIC_URL=http://localhost:3000 DOMINION_ENV_FILE=infra/.env \
    scripts/smoke-test.sh
```

The `down -v` is destructive (wipes postgres + openfga volumes) but
intended — the demo droplet carries only test data.

---

## 4. Questions you'll get — rehearsed answers

**Q: Where's the LLM?**
A: Astrid calls Claude (`claude-haiku-4-5-20251001` by default)
   through the gateway's document tools. For the demo we're using
   the deterministic stub so the draft is predictable; flip
   `DOMINION_ANTHROPIC_API_KEY` in `.env` and you get real model
   output.

**Q: How is this different from MCP?**
A: MCP standardises _how_ tools are called. Dominion governs
   _whether_ they should be. Every MCP tool call would route through
   this gateway: same auth, same ACL, same audit. They're layered,
   not competing.

**Q: What about CA rotation?**
A: Persistent CA goes in `DOMINION_CA_CERT_PEM` + `_KEY_PEM` (HSM-
   or Vault-backed in a real deploy). Agent certs are 1-year leaves;
   the CA is 10-year. Rotating the CA breaks every outstanding
   agent cert — that's why it refuses to start in production mode
   unset (Sprint 2 #9).

**Q: Can the agent skip the gateway?**
A: Not without a client cert signed by the Dominion CA. That cert's
   thumbprint is checked against the `agents` table on every
   request, so a revoked agent's cached connection dies on its next
   call. The spec's non-negotiable rule is "every agent tool call
   through the gateway"; if that breaks, the governance claim
   collapses, and Sprint 1 #3 extended it to the scheduled paths
   too.

**Q: What happens if the scheduler ran a pass I didn't see?**
A: Same answer — there's an audit row with actor = `agent:<id>`,
   on_behalf_of = the user, action = `triage.draft_created`. Filter
   by that actor and you see everything the PA did overnight, even
   though no human clicked anything.

**Q: Can I double-click approve and send twice?**
A: No. The state machine is CAS: pending → sending → sent. The
   second click sees status != pending and gets 409. The mail
   leaves exactly once, even under a flaky network or a browser
   retry. Sprint 1 #5.

**Q: What if an admin is compromised?**
A: Rotate `DOMINION_ADMIN_TOKEN` in `infra/.env` and redeploy — the
   old token stops working on the next request. Post-incident,
   every admin action the compromised token took is in the audit
   log under `actor=admin:root` (Sprint 1 #2), so the scope of the
   breach is bounded by the timestamps, not guesswork.
