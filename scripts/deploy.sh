#!/usr/bin/env bash
# Dominion MVP deploy — phases 0-2.
#
# Usage (run on the target host, from anywhere in the repo):
#   scripts/deploy.sh [--with-mock-oidc] [--skip-smoke]
#
# What it does:
#   1. Sanity-checks docker + docker compose.
#   2. Creates infra/.env from .env.example if missing.
#   3. Generates DOMINION_SESSION_SECRET and DOMINION_SCIM_TOKEN if blank.
#   4. Builds + starts the docker compose stack (gateway + postgres).
#      With --with-mock-oidc also starts the bundled navikt mock IdP.
#   5. Waits for /health to return 200.
#   6. Runs scripts/smoke-test.sh against the running gateway.
#
# Safe to re-run: all steps are idempotent.

set -euo pipefail

# --- locate repo root ---
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
repo_root="$(cd -- "${script_dir}/.." &>/dev/null && pwd)"
cd "${repo_root}"

log() { printf '\033[1;34m[deploy]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[deploy:warn]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[deploy:err]\033[0m %s\n' "$*" >&2; exit 1; }

# --- args ---
WITH_MOCK_OIDC=0
SKIP_SMOKE=0
for arg in "$@"; do
    case "$arg" in
        --with-mock-oidc) WITH_MOCK_OIDC=1 ;;
        --skip-smoke)     SKIP_SMOKE=1 ;;
        -h|--help)
            sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) die "unknown arg: $arg" ;;
    esac
done

# --- prerequisites ---
command -v docker >/dev/null 2>&1 || die "docker is not installed"
docker compose version >/dev/null 2>&1 \
    || die "docker compose plugin missing (install docker-compose-plugin)"
command -v curl >/dev/null 2>&1 || die "curl is required"

# --- env ---
env_file="infra/.env"
if [[ ! -f "${env_file}" ]]; then
    log "creating ${env_file} from .env.example"
    cp infra/.env.example "${env_file}"
fi

# Generate a secret if the var is missing or empty in the env file.
ensure_secret() {
    local key="$1" nbytes="${2:-32}"
    local current
    current="$(grep -E "^${key}=" "${env_file}" | head -n1 | cut -d= -f2- || true)"
    if [[ -z "${current}" ]]; then
        local val
        val="$(openssl rand -base64 "${nbytes}" 2>/dev/null \
               || head -c "${nbytes}" /dev/urandom | base64)"
        val="${val//[$'\n\r ']}"
        if grep -qE "^${key}=" "${env_file}"; then
            # in-place replace without sed -i portability headaches
            python3 - "${env_file}" "${key}" "${val}" <<'PY'
import sys, pathlib
p, key, val = sys.argv[1], sys.argv[2], sys.argv[3]
lines = pathlib.Path(p).read_text().splitlines()
out = []
for line in lines:
    if line.startswith(key + "="):
        out.append(key + "=" + val)
    else:
        out.append(line)
pathlib.Path(p).write_text("\n".join(out) + "\n")
PY
        else
            printf '%s=%s\n' "${key}" "${val}" >>"${env_file}"
        fi
        log "generated ${key}"
    fi
}
ensure_secret DOMINION_SESSION_SECRET 32
ensure_secret DOMINION_SCIM_TOKEN 24
ensure_secret POSTGRES_PASSWORD 18

chmod 600 "${env_file}"

# --- compose up ---
compose=(docker compose --env-file "${env_file}" -f infra/docker-compose.yml)
if [[ "${WITH_MOCK_OIDC}" -eq 1 ]]; then
    compose+=(--profile dev)
fi

log "pulling base images"
"${compose[@]}" pull --ignore-buildable-images || warn "pull partially failed; continuing"

log "building gateway image"
"${compose[@]}" build gateway

log "starting stack"
"${compose[@]}" up -d

# --- wait for /health ---
log "waiting for gateway /health ..."
public_url="$(grep -E '^DOMINION_PUBLIC_URL=' "${env_file}" | cut -d= -f2- || true)"
public_url="${public_url:-http://localhost:3000}"

for attempt in {1..60}; do
    if curl -fsS --max-time 2 "${public_url}/health" >/dev/null 2>&1; then
        log "gateway is healthy at ${public_url}"
        break
    fi
    if [[ "${attempt}" -eq 60 ]]; then
        "${compose[@]}" logs gateway | tail -40 || true
        die "gateway did not become healthy within 2 minutes"
    fi
    sleep 2
done

# --- smoke tests ---
if [[ "${SKIP_SMOKE}" -eq 1 ]]; then
    log "--skip-smoke set; leaving stack running"
    exit 0
fi

log "running Phase 1+2 smoke tests"
DOMINION_PUBLIC_URL="${public_url}" \
DOMINION_ENV_FILE="${env_file}" \
    scripts/smoke-test.sh

log "deploy complete — stack running at ${public_url}"
