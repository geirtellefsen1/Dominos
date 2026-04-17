# JR — update the `Dominos` droplet to Phase 5 (AI identity + mTLS)

This prompt assumes you have already successfully deployed Phase 3 and
Phase 4 on the `Dominos` DigitalOcean droplet at `139.59.173.17` and that
your SSH / DO credentials are already in your vault.

## What changed since your last deploy

- **Phase 5** adds AI-identity primitives: the gateway now runs a second
  listener on `:3443` with mTLS, plus routes to mint agent certificates
  (`POST /admin/agents`) and expose the Dominion CA cert
  (`GET /admin/ca/cert`).
- The agent runtime (`packages/agent-runtime`) ships a minimal Go CLI
  that authenticates to the gateway with a client cert and calls
  `/documents/*` over mTLS. This is **not** yet a fork of OpenClaw —
  that surgery lands later. What matters is that every agent tool call
  already flows through the Dominion gateway.
- Smoke test gains `5a–5g` checks: create agent → `agent read` without
  grant expects 403 → admin grant reader → `agent read` expects 200 →
  `agent write draft.v1` expects 201 → audit bundle must contain ≥ 3
  rows with `actor=agent:<id>`.
- Total smoke tests after Phase 5: roughly 22 green lines.

## Clarification on the earlier Phase-4 confusion

Your last report flagged `/audit/public-key` as 404. That was not an
implementation gap — that path simply doesn't exist in this codebase.
The two ways to get the audit public key are:

1. Inline in every export bundle:
   ```sh
   curl -H "Authorization: Bearer $DOMINION_ADMIN_TOKEN" \
        "http://127.0.0.1:3000/admin/audit?from=2026-04-17T00:00:00Z" \
        | python3 -c 'import json,sys;print(json.load(sys.stdin)["public_key_base64"])'
   ```
2. Standalone endpoint `GET /admin/audit/key` (bearer-authed):
   ```sh
   curl -H "Authorization: Bearer $DOMINION_ADMIN_TOKEN" \
        http://127.0.0.1:3000/admin/audit/key
   ```
   Response includes `public_key_base64`, `key_id`, and
   `algorithm: "Ed25519"`. (Signatures are Ed25519, not HMAC-SHA256.)

Do not add a `/audit/public-key` route — the spec's §3.7 endpoint is
already in place at `/admin/audit/key`.

## Task

Pull the branch, apply the two new prerequisites below, redeploy, and
confirm the Phase 5 smoke tests pass. Stop and report if anything is
unclear.

## Steps

1. **Connect.** SSH into the `Dominos` droplet as usual.

2. **Install the new prereq for Phase 5.** The smoke test builds the
   agent CLI locally:

   ```sh
   sudo apt update
   sudo apt install -y golang-go
   go version   # expect go1.22+ (anything 1.22+ works for a local build)
   ```

   If `apt` refuses or the go version is older than 1.22, **stop and
   report** — do not add third-party repositories.

3. **Pull the branch.**

   ```sh
   cd /opt/dominion
   git fetch origin
   git checkout claude/dominion-mvp-architecture-bNsDx
   git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
   git log --oneline -5
   ```

   You should see a commit titled "Phase 5: AI identity + mTLS + minimal
   agent CLI" at or near the top.

4. **Add the droplet's public IP to the TLS cert SAN.** So the
   auto-issued server cert on `:3443` is valid for off-host agents:

   ```sh
   grep -q '^DOMINION_TLS_IPS=' infra/.env \
     && sed -i 's|^DOMINION_TLS_IPS=.*|DOMINION_TLS_IPS=139.59.173.17|' infra/.env \
     || echo 'DOMINION_TLS_IPS=139.59.173.17' >> infra/.env
   grep '^DOMINION_TLS_' infra/.env
   ```

   Expect:
   ```
   DOMINION_TLS_ENABLED=true
   DOMINION_TLS_HOSTNAMES=
   DOMINION_TLS_IPS=139.59.173.17
   ```

5. **Redeploy.**

   ```sh
   scripts/deploy.sh --with-mock-oidc
   ```

   `deploy.sh` is idempotent and already `--force-recreate`s the
   gateway, so env changes take effect. The script will:

   - pull updated base images,
   - rebuild the gateway container (new migration 005, new CA code,
     new TLS listener),
   - start the stack,
   - wait for `/health`,
   - run `scripts/smoke-test.sh` which now includes the Phase 5 block.

   Expect `all smoke tests passed (phases 1, 2, 3, 4, 5)` at the end.

6. **Verify.** Capture this output and include it in your report:

   ```sh
   curl -s http://127.0.0.1:3000/health
   docker compose --env-file infra/.env -f infra/docker-compose.yml ps
   curl -ks https://127.0.0.1:3443/health   # TLS listener, should also say status: ok
   curl -s http://127.0.0.1:3000/admin/ca/cert | head -2   # CA cert PEM
   ```

## Report back

Post a single reply with:

1. The commit SHA at the top of the branch (`git log -1 --format=%H`).
2. The full list of smoke-test `pass` / `fail` lines — there should be
   ~22 `ok` lines and zero `fail` lines.
3. `docker compose ps` output — expect `dominion-gateway` healthy with
   both ports `3000` and `3443` published.
4. First ~3 lines of `GET /admin/ca/cert` so we can confirm the CA is
   alive.
5. Anything you had to skip or any warning you saw.

## Do NOT

- Do not commit, push, or modify the branch.
- Do not run `docker system prune`, `docker volume rm`, or any command
  that could delete the postgres data volume. OpenFGA's in-memory
  datastore will be cleared by `--force-recreate`; that's expected —
  the smoke test re-seeds what it needs.
- Do not open port 3443 on the droplet's public firewall without
  checking with the human first. The TLS listener is meant for agent
  traffic, not browsers, and currently uses a CA that isn't in any
  public trust store.
- Do not edit `infra/.env` to change `DOMINION_CA_CERT_PEM` or
  `DOMINION_CA_KEY_PEM`. The ephemeral CA regenerated on this boot is
  fine for the smoke test. Persisting the CA is a separate follow-up
  from the human.
- Do not add an `agent` service to `docker-compose.yml`. The agent CLI
  is built locally by the smoke test; no long-running container.
- Do not skip tests on failure. If the smoke test exits non-zero, leave
  the stack running and report the last ~80 lines of
  `docker compose logs gateway` so the human can see what broke.

## If something fails

- **Go build step fails:** run `go env` and include the output. If it
  shows go < 1.22, mention the distro version.
- **Agent `read` doesn't get 403 without grant:** the mTLS principal
  lookup may not be wired. Share the gateway logs for the last 30s
  (`docker compose logs --tail=200 gateway`).
- **`/admin/ca/cert` returns 404:** the agents handler didn't register.
  Share `docker compose logs gateway 2>&1 | head -60`.
- **TLS handshake failures from the agent CLI:** verify the IP in
  `DOMINION_TLS_IPS` matches the one the agent is hitting. If the agent
  connects via `127.0.0.1:3443`, the default server cert SAN already
  covers it — you shouldn't need the public IP unless running from off
  the droplet.
