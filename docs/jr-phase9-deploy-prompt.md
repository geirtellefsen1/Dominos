# JR — update the `Dominos-Server-01` droplet to Phase 9 + deliver the demo manual

Paste this prompt into JR. It's the final MVP deploy: Phase 9 adds the
admin UI and CORS middleware, a handful of targeted fixes land
alongside it, and the demo manual gets delivered to Geir on Telegram.

## Server

- Name: `Dominos-Server-01`
- Public IPv4: `139.59.173.17`
- Repo path on droplet: `/opt/dominion`
- Branch: `claude/dominion-mvp-architecture-bNsDx`

## What changed since the Phase 8 deploy

1. **Phase 9 admin UI** — a Next.js single-page dashboard at
   `packages/admin-ui`, served as a new `admin-ui` compose service on
   port `3100`. Six tabs: Agents, Users, ACL, Audit, Queue, Triage.
2. **CORS middleware** on the gateway — reads
   `DOMINION_CORS_ORIGIN`, allowlists browser origins, handles OPTIONS
   preflights. Outermost middleware so preflights never touch auth.
3. **Audit middleware order fix** (`c33c1e6`) — `auth.Attach` now
   wraps `audit.Middleware` instead of the other way round. Every
   event now records the real principal instead of
   `actor=anonymous`.
4. **Smoke-test temp-file fix** (`8b46cfb`) — large audit bundles
   (~1MB) are curl'd to `/tmp/dominion-audit-bundle.json` and Python
   reads from the file instead of stdin, so bash's argv limit no
   longer truncates them.
5. **Demo manual** at `docs/demo-manual.md` — a 5-minute live-demo
   script for the hero flow.

## Task

- Update the droplet to the head of the branch.
- Turn on CORS for the droplet's public IP so the UI can call the
  gateway from a phone.
- Open the firewall for port `3100` (TCP) so the demo device can
  reach the UI — but only that port.
- Verify the full stack is healthy and the smoke test still passes.
- Send `docs/demo-manual.md` to Geir on Telegram.

## Steps

### 1. Connect + update

```sh
ssh dominos-server   # your usual alias
cd /opt/dominion
git fetch origin
git checkout claude/dominion-mvp-architecture-bNsDx
git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
git log -1 --format='%h %s'
```

Expect the commit title to be something like `docs: 5-minute demo
manual for the MVP hero flow` or later. If `git pull` is not a
fast-forward, **stop and report**.

### 2. Set the CORS origin

The UI runs on `:3100` from the droplet's public IP, so the gateway
must emit CORS headers for that exact origin:

```sh
grep -q '^DOMINION_CORS_ORIGIN=' infra/.env \
    && sed -i 's|^DOMINION_CORS_ORIGIN=.*|DOMINION_CORS_ORIGIN=http://139.59.173.17:3100|' infra/.env \
    || echo 'DOMINION_CORS_ORIGIN=http://139.59.173.17:3100' >> infra/.env
grep '^DOMINION_CORS_ORIGIN=' infra/.env
```

Expect: `DOMINION_CORS_ORIGIN=http://139.59.173.17:3100`.

### 3. Deploy

```sh
scripts/deploy.sh --with-mock-oidc
```

This build will take noticeably longer than the previous runs — it's
the first time compose has built the `admin-ui` image (`npm install`
+ `next build`). Expect 3–6 minutes on the droplet. Do **not** kill
it; if it reports `deploy complete` at the end you're good.

The smoke test at the end should say `all smoke tests passed (phases
1, 2, 3, 4, 5, 6, 7, 8)` with ~43 green `ok` lines. If any fails,
leave the stack running, capture the last 40 lines, and report.

### 4. Open the UI port on the firewall

The UI is useless from the demo device if port 3100 is blocked.
**Only open 3100 — leave 3443 closed** (that's the mTLS agent port;
the CA isn't in any public trust store).

On `ufw`:

```sh
sudo ufw status numbered
# if 3100 is not already allowed:
sudo ufw allow 3100/tcp comment 'Dominion admin UI (demo only)'
sudo ufw status
```

If the droplet uses DigitalOcean's cloud firewall instead of ufw,
**stop and ask the human** — don't touch the cloud firewall without
explicit approval.

### 5. Verify end-to-end from outside the droplet

Exit the SSH session, then from your local shell:

```sh
curl -s http://139.59.173.17:3000/health
# → {"status":"ok"}

curl -sI http://139.59.173.17:3100/
# → HTTP/1.1 200 OK  (Next.js serves the SPA shell)

# CORS preflight check (from your local shell, not from the droplet):
curl -si -X OPTIONS http://139.59.173.17:3000/admin/agents \
  -H 'Origin: http://139.59.173.17:3100' \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: authorization,content-type' \
  | head -10
# → HTTP/1.1 204 No Content
# → Access-Control-Allow-Origin: http://139.59.173.17:3100
# → Access-Control-Allow-Credentials: true
```

If any of those fail, capture the output and report.

### 6. Deliver the demo manual to Geir on Telegram

The manual is already on the droplet at
`/opt/dominion/docs/demo-manual.md`.

- Grab the file contents:

  ```sh
  cat /opt/dominion/docs/demo-manual.md
  ```

- Send it to Geir on Telegram **as a markdown file attachment** (not
  as an inline message — it is ~5KB of markdown with tables and would
  be unreadable pasted into chat). File name: `dominion-demo-manual.md`.

- Along with the file, send one short message:

  > Dominion MVP Phase 9 deployed on Dominos-Server-01. Admin UI at
  > http://139.59.173.17:3100, open from a phone. Demo manual
  > attached — six screens, one sentence each.

- Do not summarise the manual, do not translate it, do not edit it.
  Send it verbatim.

## Report back

Post a single reply with:

1. Commit SHA at `HEAD`: `git log -1 --format=%H`.
2. Tail of the smoke-test output — the last 40 lines.
3. `docker compose --env-file infra/.env -f infra/docker-compose.yml
   ps` showing five healthy containers (`dominion-gateway`,
   `dominion-postgres`, `dominion-openfga`, `dominion-mock-oidc`,
   `dominion-admin-ui`).
4. HTTP response headers of the CORS preflight check from step 5.
5. Confirmation that the demo manual was delivered on Telegram
   (include the message timestamp).
6. Anything skipped, any warning.

## Do NOT

- Do not commit, push, or modify the branch. The manual and the UI
  are both read-only deliverables from this deploy.
- Do not open the firewall for port `3000` or `3443`. Only `3100`.
  `:3000` is plain HTTP; `:3443` is mTLS with a private CA. Neither
  is safe to expose publicly for real data.
- Do not edit `docs/demo-manual.md` before sending. If Geir needs a
  different version he'll ask.
- Do not run `docker compose down -v` this time. We want the audit
  log and the seeded alice/bob/Astrid state to survive for the demo.
- Do not leave the firewall open for `3100` after the demo. Once
  Geir confirms the demo is done, close it:
  `sudo ufw delete allow 3100/tcp`.
- Do not summarise, compress, or paraphrase the demo manual when
  sending to Telegram. Attach the file verbatim.

## If something fails

| Symptom                                             | What to do                                                                                                            |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `admin-ui` image build fails during `npm install`   | capture the last 80 lines of the `docker compose build admin-ui` output and report; likely a dep version pin issue    |
| `admin-ui` builds but crash-loops on start          | `docker compose logs admin-ui \| tail -60`; common cause is the `.next/standalone` copy missing a file               |
| CORS preflight returns 404 or no CORS headers       | check that `DOMINION_CORS_ORIGIN` made it into the running gateway: `docker exec dominion-gateway env \| grep CORS`  |
| UI loads but every API call fails with CORS error   | the Origin header in the browser doesn't match `DOMINION_CORS_ORIGIN` exactly (trailing slash, http vs https); fix the env var and redeploy |
| Telegram attach fails                               | report the error; do not paste the manual inline as a fallback — we want the file                                     |
