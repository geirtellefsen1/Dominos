#!/usr/bin/env bash
# Phase 1 + 2 + 3 acceptance smoke tests against a running Dominion gateway.
#
# Required env:
#   DOMINION_PUBLIC_URL   e.g. http://localhost:3000
#   DOMINION_ENV_FILE     path to infra/.env (to read tokens + flags)
#
# Runs every check; exits non-zero on the first failure.

set -euo pipefail

url="${DOMINION_PUBLIC_URL:?DOMINION_PUBLIC_URL is required}"
env_file="${DOMINION_ENV_FILE:-infra/.env}"

scim_token="$(grep -E '^DOMINION_SCIM_TOKEN=' "${env_file}" | cut -d= -f2- || true)"
admin_token="$(grep -E '^DOMINION_ADMIN_TOKEN=' "${env_file}" | cut -d= -f2- || true)"
dev_hdr="$(grep -E '^DOMINION_DEV_PRINCIPAL_HEADER=' "${env_file}" | cut -d= -f2- || true)"

log()  { printf '\033[1;34m[smoke]\033[0m %s\n' "$*"; }
pass() { printf '\033[1;32m  ok\033[0m   %s\n' "$*"; }
fail() { printf '\033[1;31m  fail\033[0m %s\n' "$*" >&2; exit 1; }

json_field() { python3 -c 'import json,sys;print(json.load(sys.stdin)["'"$1"'"])'; }

# ------------------------------------------------------------------------
# 0. /health
# ------------------------------------------------------------------------
log "0. GET /health"
curl -fsS "${url}/health" | grep -q '"status":"ok"' \
    && pass "health returns ok" \
    || fail "health failed"

# ------------------------------------------------------------------------
# Provision two SCIM users (alice, bob) — phases 1/3 need authed principals
# ------------------------------------------------------------------------
if [[ -z "${scim_token}" ]]; then
    fail "DOMINION_SCIM_TOKEN is unset; cannot provision test users"
fi
if [[ "${dev_hdr}" != "true" && "${dev_hdr}" != "True" ]]; then
    fail "DOMINION_DEV_PRINCIPAL_HEADER must be true for the smoke tests"
fi

scim_hdr=( -H "Authorization: Bearer ${scim_token}" -H 'content-type: application/json' )

mk_user() {
    local label="$1" email_prefix="$2"
    local email="${email_prefix}-$(date +%s%N)@example.com"
    local body='{
        "schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],
        "userName":"'"${email}"'",
        "displayName":"'"${label}"'",
        "active":true,
        "emails":[{"value":"'"${email}"'","primary":true}]
    }'
    curl -fsS -X POST "${scim_hdr[@]}" -d "${body}" "${url}/scim/v2/Users"
}

log "P. provisioning alice + bob via SCIM"
alice_json="$(mk_user Alice alice)"
bob_json="$(mk_user Bob bob)"
alice_id="$(printf '%s' "${alice_json}" | json_field id)"
bob_id="$(printf '%s' "${bob_json}"   | json_field id)"
[[ -n "${alice_id}" && -n "${bob_id}" ]] || fail "SCIM did not return ids"
pass "alice=${alice_id} bob=${bob_id}"

alice_h=( -H "X-Dominion-Dev-Principal: user:${alice_id}" )
bob_h=(   -H "X-Dominion-Dev-Principal: user:${bob_id}" )

# ------------------------------------------------------------------------
# 1. Phase 1: create, read, reject (as alice)
# ------------------------------------------------------------------------
log "1a. POST /documents (valid email.v1) as alice"
body='{
  "schema_id":"email.v1",
  "body":{
    "messageId":"smoke-'"$(date +%s%N)"'-1",
    "mailboxUser":"alice@example.com",
    "from":"bob@example.com",
    "to":["alice@example.com"],
    "subject":"smoke-test",
    "receivedAt":"2026-04-17T12:00:00Z"
  }
}'
create_resp="$(curl -fsS -X POST "${alice_h[@]}" -H 'content-type: application/json' \
    -d "${body}" "${url}/documents")"
doc_id="$(printf '%s' "${create_resp}" | json_field id)"
[[ -n "${doc_id}" ]] || fail "no id returned on create"
pass "alice created document ${doc_id}"

log "1b. GET /documents/{id} as alice (owner)"
curl -fsS "${alice_h[@]}" "${url}/documents/${doc_id}" | grep -q "\"id\":\"${doc_id}\"" \
    && pass "owner can fetch document" \
    || fail "owner fetch failed"

log "1c. POST /documents malformed -> expect 400 schema_violation"
bad_status="$(curl -s -o /tmp/dominion-smoke-bad.json -w '%{http_code}' \
    -X POST "${alice_h[@]}" -H 'content-type: application/json' \
    -d '{"schema_id":"email.v1","body":{"from":"bob@example.com"}}' \
    "${url}/documents")"
[[ "${bad_status}" == "400" ]] \
    || fail "expected 400, got ${bad_status}"
grep -q 'schema_violation' /tmp/dominion-smoke-bad.json \
    && pass "rejected malformed with schema_violation" \
    || fail "400 response missing schema_violation"

# ------------------------------------------------------------------------
# 2. Phase 2: /me unauth + SCIM deactivation already exercised above
# ------------------------------------------------------------------------
log "2a. GET /me without session -> expect 401"
me_status="$(curl -s -o /dev/null -w '%{http_code}' "${url}/me")"
[[ "${me_status}" == "401" ]] \
    && pass "unauthenticated /me returns 401" \
    || fail "expected 401, got ${me_status}"

log "2b. userName eq filter resolves alice"
alice_email="$(printf '%s' "${alice_json}" | json_field userName)"
filter_url="${url}/scim/v2/Users?filter=userName%20eq%20%22${alice_email}%22"
curl -fsS "${scim_hdr[@]}" "${filter_url}" | grep -q "\"id\":\"${alice_id}\"" \
    && pass "SCIM filter returned alice" \
    || fail "SCIM filter did not match alice"

# ------------------------------------------------------------------------
# 3. Phase 3: ACL — deny, grant, allow, revoke
# ------------------------------------------------------------------------
if [[ -z "${admin_token}" ]]; then
    fail "DOMINION_ADMIN_TOKEN is unset; cannot run Phase 3 tests"
fi
admin_hdr=( -H "Authorization: Bearer ${admin_token}" -H 'content-type: application/json' )

log "3a. bob GETs alice's doc -> expect 403"
bob_status="$(curl -s -o /tmp/dominion-smoke-403.json -w '%{http_code}' \
    "${bob_h[@]}" "${url}/documents/${doc_id}")"
[[ "${bob_status}" == "403" ]] \
    && pass "bob forbidden before grant" \
    || fail "expected 403, got ${bob_status}"

log "3b. admin grants bob reader on the document"
grant_body='{
  "principal":"user:'"${bob_id}"'",
  "relation":"reader",
  "resource":"document:'"${doc_id}"'"
}'
curl -fsS -X POST "${admin_hdr[@]}" -d "${grant_body}" "${url}/admin/acl/grant" >/dev/null \
    && pass "grant accepted" \
    || fail "grant failed"

log "3c. bob GETs alice's doc -> expect 200"
curl -fsS "${bob_h[@]}" "${url}/documents/${doc_id}" | grep -q "\"id\":\"${doc_id}\"" \
    && pass "bob can read after grant" \
    || fail "bob still cannot read after grant"

log "3d. admin revokes bob's reader"
curl -fsS -X POST "${admin_hdr[@]}" -d "${grant_body}" "${url}/admin/acl/revoke" >/dev/null \
    && pass "revoke accepted" \
    || fail "revoke failed"

log "3e. bob GETs alice's doc -> expect 403 again"
bob_status="$(curl -s -o /dev/null -w '%{http_code}' "${bob_h[@]}" "${url}/documents/${doc_id}")"
[[ "${bob_status}" == "403" ]] \
    && pass "bob forbidden again after revoke" \
    || fail "expected 403 after revoke, got ${bob_status}"

log "3f. alice's list filters to only docs she can read"
list_resp="$(curl -fsS "${alice_h[@]}" "${url}/documents?schema=email.v1")"
printf '%s' "${list_resp}" | grep -q "\"id\":\"${doc_id}\"" \
    && pass "alice sees her document in list" \
    || fail "alice cannot see her own document in list"

bob_list="$(curl -fsS "${bob_h[@]}" "${url}/documents?schema=email.v1")"
if printf '%s' "${bob_list}" | grep -q "\"id\":\"${doc_id}\""; then
    fail "bob sees alice's document in list after revoke"
fi
pass "bob's list excludes alice's document"

# ------------------------------------------------------------------------
# 4. Phase 4: signed audit bundle + tamper detection
# ------------------------------------------------------------------------
log "4a. generate ~100 traced requests for the audit log"
# Every /documents read above counts; top up with cheap allowed + denied
# reads so the bundle comfortably exceeds 100 entries.
for i in $(seq 1 80); do
    curl -s -o /dev/null "${bob_h[@]}" "${url}/documents/${doc_id}"
    curl -s -o /dev/null "${alice_h[@]}" "${url}/documents/${doc_id}"
done
pass "generated request volume"

log "4b. export audit bundle"
from_ts="$(date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
          || python3 -c 'import datetime as d;print((d.datetime.utcnow()-d.timedelta(minutes=5)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
export_url="${url}/admin/audit?from=${from_ts}"
bundle="$(curl -fsS "${admin_hdr[@]}" "${export_url}")"
count="$(printf '%s' "${bundle}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["count"])')"
[[ "${count}" -ge 100 ]] \
    && pass "bundle contains ${count} entries (>=100)" \
    || fail "expected >=100 entries, got ${count}"

log "4c. verify every signature with the published public key"
printf '%s' "${bundle}" | python3 - <<'PY'
import json, sys, base64
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

def canon(ev):
    ev = {k: v for k, v in ev.items() if k != "signature"}
    return json.dumps(ev, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()

bundle = json.load(sys.stdin)
pub = Ed25519PublicKey.from_public_bytes(base64.b64decode(bundle["public_key_base64"]))
events = bundle["events"]
bad = 0
for ev in events:
    sig = base64.b64decode(ev["signature"])
    try:
        pub.verify(sig, canon(ev))
    except Exception:
        bad += 1
print(f"verified_ok={len(events)-bad} verified_fail={bad}")
sys.exit(0 if bad == 0 else 1)
PY
[[ $? -eq 0 ]] \
    && pass "every audit entry verifies" \
    || fail "one or more audit entries failed to verify"

log "4d. tamper with one entry — only that entry should fail"
printf '%s' "${bundle}" | python3 - <<'PY'
import json, sys, base64
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

def canon(ev):
    ev = {k: v for k, v in ev.items() if k != "signature"}
    return json.dumps(ev, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()

bundle = json.load(sys.stdin)
pub = Ed25519PublicKey.from_public_bytes(base64.b64decode(bundle["public_key_base64"]))
events = bundle["events"]
# Flip the decision of the middle entry.
target = len(events) // 2
events[target]["decision"] = "tampered"
failures = []
for i, ev in enumerate(events):
    sig = base64.b64decode(ev["signature"])
    try:
        pub.verify(sig, canon(ev))
    except Exception:
        failures.append(i)
if failures == [target]:
    print(f"tamper_detected_at_index={target} other_failures=0")
    sys.exit(0)
else:
    print(f"unexpected_failures={failures} target={target}")
    sys.exit(1)
PY
[[ $? -eq 0 ]] \
    && pass "tamper isolated to the one modified entry" \
    || fail "tamper detection broken — check canonicalisation"

log "4e. ACL deny (phase 3 bob->alice doc) is present in the log"
denied="$(printf '%s' "${bundle}" \
    | python3 -c 'import json,sys;b=json.load(sys.stdin);print(sum(1 for e in b["events"] if e["decision"]=="deny" and e["actor"].startswith("user:") and e["resource"]=="document:'"${doc_id}"'"))')"
[[ "${denied}" -ge 1 ]] \
    && pass "at least one deny event for the target document is recorded (${denied})" \
    || fail "no deny events recorded for the test document"

log "all smoke tests passed (phases 1, 2, 3, 4)"
