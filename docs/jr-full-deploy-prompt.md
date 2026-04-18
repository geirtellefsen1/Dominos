# JR — full redeploy of Dominion MVP (phases 0–8) on `Dominos-Server-01`

Paste this prompt into JR as-is. It supersedes every earlier deploy
prompt. Use it when JR is in a mixed state, when the droplet has gone
through crash loops, or whenever we want a guaranteed-clean bring-up
of phases 0 through 8 end-to-end.

---

## Server

- Name: `Dominos-Server-01`
- Public IPv4: `139.59.173.17`
- Private IP: `10.106.0.3`
- Repo checkout path on the droplet: `/opt/dominion`
- Branch: `claude/dominion-mvp-architecture-bNsDx`

## Why this prompt is different from the earlier ones

The droplet currently has Phase 7 code checked out (commit `9ae1d19`)
and the audit log is crash-looping signature verification. That is a
fixed bug: commit `d08848b` (a Phase 6 follow-up) truncates the audit
timestamp to microseconds before signing so Postgres's `TIMESTAMPTZ`
round-trip no longer invalidates Ed25519 signatures. Phase 8
(one-revoke offboarding) has also landed on the branch (`9252f98`),
and a smoke-test fix that writes large audit bundles through a temp
file instead of bash-interpolating them into Python heredocs. All
three land with a single `git pull`. **Do not patch the code on the
droplet by hand** — pull the branch instead.

## Task

Redeploy phases 1–8 of the Dominion MVP on `Dominos-Server-01`. Stop
and ask the human if anything is unclear or if any step requires
opening a firewall port, rotating a real credential, or running a
destructive docker command beyond the sanctioned `compose down -v`.

## Prerequisites (install once, skip if already present)

SSH in as the usual operator user, then:

```sh
docker --version              # 24+
docker compose version        # v2
git --version
curl --version
python3 --version             # 3.10+
openssl version
python3 -c 'import cryptography; print(cryptography.__version__)'
go version                    # 1.22+  (needed to build the agent CLI for phase 5)
```

If any of these are missing or too old:

```sh
sudo apt update
sudo apt install -y docker.io docker-compose-plugin git curl python3 python3-cryptography openssl golang-go
```

If `apt` refuses or can't find a recent `golang-go`, **stop and
report** — do not add third-party apt sources without a human.

## Clean slate (only when the previous run is wedged)

The current droplet is mid-crash-loop with possibly-corrupted secrets
in `infra/.env`. Do a full reset:

```sh
cd /opt/dominion
docker compose --env-file infra/.env -f infra/docker-compose.yml down -v   # destroys postgres + openfga volumes
rm -f infra/.env                                                            # force fresh secret generation
```

`-v` is destructive but intentional here — the only data on the
droplet is test data from earlier smoke runs. Do **not** add `-v` to
the routine redeploy flow below.

## Update the branch

```sh
cd /opt/dominion
git fetch origin
git checkout claude/dominion-mvp-architecture-bNsDx
git pull --ff-only origin claude/dominion-mvp-architecture-bNsDx
git log --oneline -6
```

Expect the first line of `git log` to be a commit titled something
like `Phase 8: one-revoke offboarding primitive` or later. If your
`git pull` reports anything other than "fast-forward" or "already
up-to-date", **stop and report** — do not force.

## Configure the droplet's public IP in `.env`

After `deploy.sh` first creates `infra/.env`, append the droplet's
public IP so the auto-issued TLS cert on `:3443` is valid for
off-host agents:

```sh
# Create .env from example if the clean-slate step deleted it.
[ -f infra/.env ] || cp infra/.env.example infra/.env

# Idempotently set the TLS IP.
grep -q '^DOMINION_TLS_IPS=' infra/.env \
    && sed -i 's|^DOMINION_TLS_IPS=.*|DOMINION_TLS_IPS=139.59.173.17|' infra/.env \
    || echo 'DOMINION_TLS_IPS=139.59.173.17' >> infra/.env

# Ensure the dev flags are on so the smoke test's simulate paths run.
for k in DOMINION_TLS_ENABLED DOMINION_DEV_PRINCIPAL_HEADER DOMINION_DEV_GRAPH_SIMULATE; do
    grep -q "^$k=" infra/.env \
        && sed -i "s|^$k=.*|$k=true|" infra/.env \
        || echo "$k=true" >> infra/.env
done

grep -E '^(DOMINION_TLS_IPS|DOMINION_TLS_ENABLED|DOMINION_DEV_PRINCIPAL_HEADER|DOMINION_DEV_GRAPH_SIMULATE)=' infra/.env
```

Expected output:
```
DOMINION_TLS_ENABLED=true
DOMINION_TLS_IPS=139.59.173.17
DOMINION_DEV_PRINCIPAL_HEADER=true
DOMINION_DEV_GRAPH_SIMULATE=true
```

## Sanity-check the secrets

The earlier crash loop was caused by malformed base64 in
`DOMINION_ENCRYPTION_KEY` and `DOMINION_SESSION_SECRET` written by an
old version of `deploy.sh`. The current `deploy.sh` generates valid
base64 with `openssl rand -base64 <N>`, but if the file already has
stale values from a previous bad run, wipe them before running:

```sh
for k in DOMINION_SESSION_SECRET DOMINION_SCIM_TOKEN DOMINION_ADMIN_TOKEN \
         DOMINION_AUDIT_PRIVATE_KEY DOMINION_ENCRYPTION_KEY POSTGRES_PASSWORD; do
    sed -i "s|^$k=.*|$k=|" infra/.env
done
grep -E '^(DOMINION_SESSION_SECRET|DOMINION_SCIM_TOKEN|DOMINION_ADMIN_TOKEN|DOMINION_AUDIT_PRIVATE_KEY|DOMINION_ENCRYPTION_KEY|POSTGRES_PASSWORD)=' infra/.env
```

All six lines should print `<name>=` (empty value). `deploy.sh` will
regenerate them in the next step.

## Deploy

```sh
cd /opt/dominion
scripts/deploy.sh --with-mock-oidc
```

`deploy.sh` is idempotent:
- fills in any empty secret in `infra/.env`,
- rebuilds the gateway image (picks up audit-sig fix, phase 8, and
  the smoke-test temp-file fix),
- `docker compose up -d --force-recreate` so env changes always
  apply,
- polls `/health` until it responds 200,
- runs `scripts/smoke-test.sh`.

Expect the tail of the output to say `all smoke tests passed (phases
1, 2, 3, 4, 5, 6, 7, 8)` — roughly 43 green `ok` lines and zero
`fail` lines.

## Verify

Capture these for the report:

```sh
curl -s http://127.0.0.1:3000/health
docker compose --env-file infra/.env -f infra/docker-compose.yml ps
curl -s http://127.0.0.1:3000/admin/ca/cert | head -2
curl -sk https://127.0.0.1:3443/health    # TLS listener should also answer
```

`/health` must return `{"status":"ok"}` on both the plain HTTP port
(`:3000`) and the TLS port (`:3443`).

## Report back

One reply containing:

1. Commit SHA at `HEAD`: `git log -1 --format=%H`.
2. Tail of the smoke test output — the last 40 lines should list
   every `ok` line from phases 1 through 8.
3. `docker compose ps` output with four healthy containers
   (`dominion-gateway`, `dominion-postgres`, `dominion-openfga`,
   `dominion-mock-oidc`).
4. Anything skipped, any warning, any retry.

## Do NOT

- Do not commit, push, or modify the branch. All fixes land via
  `git pull` from the human's workstation.
- Do not run `docker volume rm`, `docker system prune -a`, or any
  variant outside the single sanctioned `compose down -v` in the
  Clean Slate section. OpenFGA's in-memory datastore gets wiped on
  every `--force-recreate`; that is expected and the smoke test
  re-seeds its tuples.
- Do not open port `3443` on the droplet's public firewall. The TLS
  listener is for agent-to-gateway calls; the CA isn't in any public
  trust store so browsers will refuse it anyway.
- Do not edit `DOMINION_CA_CERT_PEM`, `DOMINION_CA_KEY_PEM`,
  `DOMINION_AUDIT_PRIVATE_KEY`, or `DOMINION_ENCRYPTION_KEY` once
  they have been generated by `deploy.sh`. Rotating these mid-flight
  breaks previously-issued agents (CA), previously-signed audit rows
  (audit key), and previously-stored refresh tokens (encryption key).
  Sanctioned flow for rotation is a clean-slate redeploy, which
  destroys the old data deliberately.
- Do not skip `--force-recreate`. Without it, compose caches old env
  values even when `infra/.env` changed — the symptom you hit on
  Phase 3.
- Do not hand-patch `scripts/smoke-test.sh` on the droplet. If the
  smoke test surfaces a real issue, report it with exact line numbers
  and the output that diverged — the human will patch and push.

## Troubleshooting cues

| Symptom | Likely cause | What to do |
| --- | --- | --- |
| Gateway container crash-loops right after start | malformed secret in `infra/.env` | run the "Sanity-check the secrets" block, then re-run `deploy.sh` |
| `4c. verify every signature ...` fails with >0 `verified_fail` | old Ed25519-signed rows with retired key_ids are in the DB | they are skipped by design (look for `skipped_other_keys=N` in the output); a non-zero `verified_fail` count is the real bug — report the count and the first failing event's `timestamp` |
| `4c` fails with `no events match current key_id=...` | `DOMINION_AUDIT_PRIVATE_KEY` regenerated on a restart | confirm the env var has a value, do a Clean Slate, redeploy |
| `5a` hangs or times out | `go build ./cmd/agent` failing | capture `go env` output; if go < 1.22, upgrade via apt |
| `5c. agent reads alice's doc WITHOUT grant -> expect 403` returns HTTP 401 instead | the agent cert was issued before the CA rotated (or the gateway restarted with an ephemeral CA) | do a Clean Slate so everything is issued under the same CA |
| `6a simulate` returns 404 | `DOMINION_DEV_GRAPH_SIMULATE` not `true` | re-run the `.env` setup block at the top |
| Smoke test hangs after writing the bundle to `/tmp/dominion-audit-bundle.json` | Python heredoc did not receive the file path — usually a bash path problem | `ls -la /tmp/dominion-audit-bundle.json` to confirm the file exists and is non-empty; if empty, the audit endpoint itself is the problem |
| `8b revoked agent gets 401` fails with 403 instead | the dev-principal active-check plug from Phase 8 is missing — you are on pre-`9252f98` code | check `git log --oneline -1` and pull |
