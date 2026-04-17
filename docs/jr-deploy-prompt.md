# JR — deploy Dominion MVP (phases 0–2) to the `Dominos` droplet

Paste this prompt into JR. It assumes JR already has SSH / DigitalOcean
credentials for the droplet named **`Dominos`** and has a shell tool
available.

---

## Context

You are deploying the **Dominion MVP** — a Go-based governance gateway that
fronts a Postgres document store + schema registry (phase 1) and
Entra-compatible OIDC + SCIM (phase 2). Full spec is on the branch in
`docs/architecture.md`. Do not read the whole spec for this task; the
deploy script handles everything.

The repo is on GitHub at `geirtellefsen1/Dominos`, branch
**`claude/dominion-mvp-architecture-bNsDx`**. The scripts live in
`scripts/deploy.sh` and `scripts/smoke-test.sh`.

## Task

Deploy phases 0, 1, and 2 of the MVP to the DigitalOcean droplet named
`Dominos` and verify it works by running the bundled smoke tests. Stop and
ask the human if anything looks off — do not improvise past the documented
flow.

## Steps

1. **Connect.** SSH into the `Dominos` droplet using the credentials you
   already have in your vault. Do not modify SSH config, firewall rules, or
   user accounts on the droplet.

2. **Check prerequisites.** Confirm these are installed:

   ```sh
   docker --version
   docker compose version
   git --version
   curl --version
   python3 --version
   openssl version
   ```

   If any of these are missing, **stop and report** — do not install new
   packages without a human approving.

3. **Get the code.** If `/opt/dominion` does not yet exist:

   ```sh
   sudo mkdir -p /opt/dominion
   sudo chown "$(whoami):$(whoami)" /opt/dominion
   git clone https://github.com/geirtellefsen1/Dominos.git /opt/dominion
   ```

   Otherwise:

   ```sh
   cd /opt/dominion
   git fetch origin
   git checkout claude/dominion-mvp-architecture-bNsDx
   git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
   ```

   If `git pull` is not a fast-forward, **stop and report** — do not run
   `git reset --hard` or force-rewrite history.

4. **Run the deploy script.** From `/opt/dominion`:

   ```sh
   scripts/deploy.sh --with-mock-oidc
   ```

   This will:
   - create `infra/.env` from the example if missing,
   - generate `DOMINION_SESSION_SECRET`, `DOMINION_SCIM_TOKEN`, and
     `POSTGRES_PASSWORD` if they are blank,
   - `docker compose build gateway`,
   - `docker compose up -d` (postgres, openfga, mock-oidc, gateway),
   - wait for `GET /health` to return `{"status":"ok"}`,
   - run `scripts/smoke-test.sh` which exercises the Phase 1 + Phase 2
     acceptance tests.

5. **Verify.** Success looks like the smoke test printing `all smoke tests
   passed` at the end. Then run one final manual probe and capture the
   output:

   ```sh
   curl -s http://127.0.0.1:3000/health
   docker compose --env-file infra/.env -f infra/docker-compose.yml ps
   ```

6. **Expose the service** (optional, only if the human has set up DNS for
   the droplet). The gateway listens on `:3000`. If a reverse proxy like
   nginx or Caddy is already installed on the droplet and has a vhost for
   `dominion.<domain>`, leave it as-is. If no proxy is configured, **do
   not** open port 3000 to the public internet — report the droplet IP and
   let the human decide.

## Report back

Post a single reply containing:

1. The droplet public IP and the URL where `/health` is reachable.
2. The final few lines of `scripts/deploy.sh` output (the `smoke tests
   passed` section).
3. `docker compose ps` output showing which services are running.
4. Anything you had to skip or any warnings you saw.

## Do NOT

- Do not commit, push, or modify the branch.
- Do not run `docker system prune`, `docker volume rm`, or anything that
  could delete the postgres data volume.
- Do not change any secret value once the stack is running — rotating
  secrets requires human coordination.
- Do not open inbound firewall rules on the droplet without human approval.
- Do not install or uninstall packages system-wide.
- Do not start any phase beyond Phase 2. Phases 3–9 are on the roadmap but
  not yet built.

## If something fails

- If `scripts/deploy.sh` exits non-zero, capture the last ~80 lines of
  `docker compose logs gateway` and the output of `docker compose ps`, then
  report to the human with that output. Do not retry more than twice.
- If `scripts/smoke-test.sh` fails partway, leave the stack running —
  **do not** `docker compose down`. The human needs to inspect the live
  state.
