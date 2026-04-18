# JR — deploy the demo UI + admin console reshuffle

Paste into JR. Four commits to pull, one rebuild, one five-field setup
in the browser when it comes back up.

## Server

- `Dominos-Server-01` · `139.59.173.17` · `/opt/dominion` · branch
  `claude/dominion-mvp-architecture-bNsDx`

## What changed

Four commits landed on the branch, all inside `packages/admin-ui/`
(the gateway is untouched):

1. `d899536` · the feature-by-feature admin dashboard moved from
   `/` to `/admin`. Same functionality, new URL.
2. `f33ce59` · new demo view at `/` — dark three-pane layout, live
   audit feed polling every 2s, governance banner that lights up
   as each of the four claims is demonstrated.
3. `0ac0d65` · Astrid pane wired to `POST /admin/triage/run` and
   `DELETE /admin/agents/{id}`.
4. `d6957de` · Inbox pane wired to `/me/queue`, `/approve`,
   `/reject` with status pills animating across pending → sending
   → sent.

No gateway API changes. No new env vars. No new dependencies.

## Task

Pull, rebuild, verify the new demo loads, then configure it once
with the three IDs from the smoke test. Report with a screenshot
of the loaded demo.

## Steps

### 1. Connect + update

```sh
ssh dominos-server
cd /opt/dominion
git fetch origin
git checkout claude/dominion-mvp-architecture-bNsDx
git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
git log -1 --format='%h %s'
```

Expect head commit `demo/phase2c: wire Inbox pane …` or later.

### 2. Redeploy

```sh
scripts/deploy.sh --with-mock-oidc
```

The `admin-ui` image rebuild takes ~2–3 min the first time since
the node_modules layer is invalidated by the file edits.
`deploy.sh` uses `--force-recreate` so the running container picks
up the new build.

### 3. Bootstrap demo data (if not already present)

The demo view needs alice's user id and Astrid's agent id. The
smoke test provisions both:

```sh
DOMINION_PUBLIC_URL=http://localhost:3000 \
DOMINION_ENV_FILE=infra/.env \
    scripts/smoke-test.sh | tee /tmp/smoke.log
```

When it finishes, pull the two ids and the admin token:

```sh
grep -E 'alice=|agent [0-9a-f]{8}' /tmp/smoke.log
grep '^DOMINION_ADMIN_TOKEN=' infra/.env
```

Write these down — you'll paste them into the browser in step 5.

### 4. Verify from outside the droplet

From your local shell:

```sh
curl -sI http://139.59.173.17:3100/        # demo landing
curl -sI http://139.59.173.17:3100/admin   # admin console (moved)
```

Both should return HTTP/1.1 200.

### 5. Configure + verify in the browser

Open `http://139.59.173.17:3100` on a demo device (laptop is
easiest; phone works too).

You should see a dark landing page with a gradient headline "Every
AI action, accountable." and a `Configure demo →` button. Click it.
Fill five fields:

| Field | Value |
| ----- | ----- |
| Gateway URL       | `http://139.59.173.17:3000` |
| Admin bearer token | the `DOMINION_ADMIN_TOKEN` from step 3 |
| Alice — user id   | the alice uuid from step 3 |
| Alice — email     | `alice@example.com` (cosmetic only) |
| Astrid — agent id | the agent uuid from step 3 |

Click **save**. Three panes should appear:

- **Left · Astrid** — gradient avatar, "active" badge, big "Run
  triage now" button, red "Revoke" button at the bottom.
- **Middle · alice's inbox** — currently shows "All caught up"
  (no pending drafts yet) with a ✉️ glyph.
- **Right · Audit stream** — live list, newest-first, each row with
  a coloured actor chip (violet / sky / amber / grey). The pulsing
  green dot in the header confirms polling is alive.

The governance banner at the top should start lighting the four
words — `auditable` first (any event lights it), then the others as
events flow.

### 6. Smoke test the flow by hand (recommended)

- Click **Run triage now** on the left pane.
  - Expect the button to say "Triaging…" for ~1s.
  - A stats card appears: `drafted / skipped / processed`.
  - A new draft appears in the inbox with an amber "pending" pill
    and a violet "drafted by Astrid" chip.
  - A new row appears in the audit stream: actor = `agent:<id>`,
    action = `triage.draft_created`.
- Click **Approve · send via Graph** on the draft.
  - Pill transitions amber → sky (pulsing) → emerald "sent ✓".
  - An emerald banner appears: "Sent via Microsoft Graph".
  - The sentMessageId (real RFC 5322 id, or `sim:…` in simulate
    mode) shows in an emerald mono chip on the card.
- Click **Revoke · one-way** on the left.
  - Confirm the browser dialog.
  - Astrid avatar grays out, status flips to "inactive", button
    disabled.
  - Red card shows `N FGA tuples deleted`.
  - Audit stream gets a new row: actor = `admin:root`, action ~
    `http.DELETE /admin/agents/{id}`.

## Report back

1. Commit SHA at HEAD: `git log -1 --format=%H`.
2. Output of the two `curl -sI` probes from step 4.
3. A screenshot of the populated demo view (all three panes with
   live data). OK if it's an iPhone screenshot — we just want to
   see the three panes rendered with a draft card + the audit
   stream.
4. Anything that didn't match the expected flow above.

## Do NOT

- Do not `docker compose down -v`. The audit log + seeded
  alice/bob/Astrid state are what the demo reads.
- Do not change anything in `infra/.env` — no env changes are
  needed for this deploy.
- Do not hardcode the IDs into the code. The demo stores them in
  browser `localStorage` on purpose so wiping the cache or
  re-bootstrapping is a one-minute reset.
- Do not delete / regenerate Astrid between the bootstrap (step 3)
  and the first demo click. If Astrid is missing you'll see
  `[404 not_found]` in the left pane's error box — re-seed via
  `scripts/smoke-test.sh`.
- Do not expose port `3100` to the public internet. It's plain HTTP
  and the admin bearer token flows in `Authorization` headers.
  Keep it VPN / SSH-tunnel only, or put a TLS terminator in front.

## If something fails

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Landing page loads but "Configure demo" does nothing | localStorage blocked (incognito + strict settings) | use a normal browser window |
| Demo loads but "audit poll error · [401 unauthorized]" banner appears | admin token typo in setup | click settings (top right), re-paste the token, save |
| "CORS blocked" in browser devtools | `DOMINION_CORS_ORIGIN` doesn't match the phone/laptop's origin exactly | on the droplet, `grep CORS infra/.env` → must be `http://139.59.173.17:3100`, then redeploy |
| Inbox says `[401 unauthorized]` when clicking approve | `DOMINION_DEV_PRINCIPAL_HEADER` isn't `true` on the gateway | `grep DEV_PRINCIPAL infra/.env` on droplet; if false, set true and redeploy |
| Astrid pane says `[404 not_found]` on revoke | the Astrid uuid in the setup modal doesn't exist; smoke test re-created her with a different uuid | settings → paste the NEW uuid from `/tmp/smoke.log` → save |
| Triage stats all zero | Alice has no recent emails in the lookback window | re-run `scripts/smoke-test.sh` on the droplet to seed a fresh simulated message |
