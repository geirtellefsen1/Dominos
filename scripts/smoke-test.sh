#!/usr/bin/env bash
# Phase 1 + 2 acceptance smoke tests against a running Dominion api-gateway.
#
# Required env:
#   DOMINION_PUBLIC_URL   e.g. http://localhost:3000
#   DOMINION_ENV_FILE     path to infra/.env (to read DOMINION_SCIM_TOKEN)
#
# Runs every check; exits non-zero on the first failure.

set -euo pipefail

url="${DOMINION_PUBLIC_URL:?DOMINION_PUBLIC_URL is required}"
env_file="${DOMINION_ENV_FILE:-infra/.env}"

scim_token="$(grep -E '^DOMINION_SCIM_TOKEN=' "${env_file}" | cut -d= -f2- || true)"

log()  { printf '\033[1;34m[smoke]\033[0m %s\n' "$*"; }
pass() { printf '\033[1;32m  ok\033[0m   %s\n' "$*"; }
fail() { printf '\033[1;31m  fail\033[0m %s\n' "$*" >&2; exit 1; }

# --- 0. /health ---
log "0. GET /health"
curl -fsS "${url}/health" | grep -q '"status":"ok"' \
    && pass "health returns ok" \
    || fail "health failed"

# --- 1. Phase 1 acceptance: create, read, reject ---
log "1a. POST /documents (valid email.v1)"
body='{
  "schema_id":"email.v1",
  "body":{
    "messageId":"smoke-'$(date +%s)'-1",
    "mailboxUser":"alice@example.com",
    "from":"bob@example.com",
    "to":["alice@example.com"],
    "subject":"smoke-test",
    "receivedAt":"2026-04-17T12:00:00Z"
  }
}'
create_resp="$(curl -fsS -X POST -H 'content-type: application/json' \
    -d "${body}" "${url}/documents")"
doc_id="$(printf '%s' "${create_resp}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')"
[[ -n "${doc_id}" ]] || fail "no id returned on create"
pass "created document ${doc_id}"

log "1b. GET /documents/{id}"
curl -fsS "${url}/documents/${doc_id}" | grep -q "\"id\":\"${doc_id}\"" \
    && pass "fetched document ${doc_id}" \
    || fail "fetch failed"

log "1c. POST /documents (malformed -> expect 400 schema_violation)"
bad_status="$(curl -s -o /tmp/dominion-smoke-bad.json -w '%{http_code}' \
    -X POST -H 'content-type: application/json' \
    -d '{"schema_id":"email.v1","body":{"from":"bob@example.com"}}' \
    "${url}/documents")"
[[ "${bad_status}" == "400" ]] \
    || fail "expected 400, got ${bad_status}"
grep -q 'schema_violation' /tmp/dominion-smoke-bad.json \
    && pass "rejected malformed with schema_violation" \
    || fail "400 response missing schema_violation"

# --- 2. Phase 2 acceptance: /me unauth + SCIM + deactivation ---
log "2a. GET /me without session -> expect 401"
me_status="$(curl -s -o /dev/null -w '%{http_code}' "${url}/me")"
[[ "${me_status}" == "401" ]] \
    && pass "unauthenticated /me returns 401" \
    || fail "expected 401, got ${me_status}"

if [[ -z "${scim_token}" ]]; then
    log "2b..2e skipped — DOMINION_SCIM_TOKEN not set in ${env_file}"
    log "smoke tests passed (partial — SCIM skipped)"
    exit 0
fi

hdr=( -H "Authorization: Bearer ${scim_token}" -H 'content-type: application/json' )

log "2b. POST /scim/v2/Users (create)"
scim_body='{
  "schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],
  "userName":"smoke-'"$(date +%s)"'@example.com",
  "displayName":"Smoke Test",
  "active":true,
  "emails":[{"value":"smoke-'"$(date +%s)"'@example.com","primary":true}]
}'
scim_resp="$(curl -fsS -X POST "${hdr[@]}" -d "${scim_body}" "${url}/scim/v2/Users")"
scim_id="$(printf '%s' "${scim_resp}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')"
[[ -n "${scim_id}" ]] || fail "SCIM create returned no id"
pass "SCIM created user ${scim_id}"

log "2c. PATCH active=false"
patch_body='{
  "schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],
  "Operations":[{"op":"replace","path":"active","value":false}]
}'
patched="$(curl -fsS -X PATCH "${hdr[@]}" -d "${patch_body}" "${url}/scim/v2/Users/${scim_id}")"
printf '%s' "${patched}" | grep -q '"active":false' \
    && pass "user ${scim_id} deactivated" \
    || fail "expected active:false in PATCH response"

log "2d. GET /scim/v2/Users/{id} reflects deactivation"
curl -fsS "${hdr[@]}" "${url}/scim/v2/Users/${scim_id}" | grep -q '"active":false' \
    && pass "deactivation persisted" \
    || fail "deactivation did not persist"

log "2e. userName eq filter resolves to the same user"
filter_url="${url}/scim/v2/Users?filter=userName%20eq%20%22$(printf '%s' "${scim_resp}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["userName"])')%22"
curl -fsS "${hdr[@]}" "${filter_url}" | grep -q "\"id\":\"${scim_id}\"" \
    && pass "filter lookup returned the user" \
    || fail "filter lookup did not match"

log "all smoke tests passed"
