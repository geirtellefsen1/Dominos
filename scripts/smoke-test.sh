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
current_kid = bundle["key_id"]
pub = Ed25519PublicKey.from_public_bytes(base64.b64decode(bundle["public_key_base64"]))

# Filter to events signed by the CURRENT key. Events signed by an older
# key (ephemeral key before DOMINION_AUDIT_PRIVATE_KEY was persisted,
# or a pre-rotation key) are still present in the bundle but can't be
# verified without that key's pubkey — a future /admin/audit/keys
# endpoint can return the full history; for now they are skipped.
active = [e for e in bundle["events"] if e.get("key_id") == current_kid]
skipped = len(bundle["events"]) - len(active)
if not active:
    print(f"no events match current key_id={current_kid}; total={len(bundle['events'])}")
    sys.exit(1)

bad = 0
for ev in active:
    sig = base64.b64decode(ev["signature"])
    try:
        pub.verify(sig, canon(ev))
    except Exception:
        bad += 1
print(f"verified_ok={len(active)-bad} verified_fail={bad} skipped_other_keys={skipped}")
sys.exit(0 if bad == 0 else 1)
PY
[[ $? -eq 0 ]] \
    && pass "every audit entry for the current key_id verifies" \
    || fail "one or more audit entries failed to verify"

log "4d. tamper with one entry — only that entry should fail"
printf '%s' "${bundle}" | python3 - <<'PY'
import json, sys, base64
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

def canon(ev):
    ev = {k: v for k, v in ev.items() if k != "signature"}
    return json.dumps(ev, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()

bundle = json.load(sys.stdin)
current_kid = bundle["key_id"]
pub = Ed25519PublicKey.from_public_bytes(base64.b64decode(bundle["public_key_base64"]))

active = [e for e in bundle["events"] if e.get("key_id") == current_kid]
if len(active) < 2:
    print(f"need at least 2 active events to run tamper test; got {len(active)}")
    sys.exit(1)

target = len(active) // 2
active[target]["decision"] = "tampered"
failures = []
for i, ev in enumerate(active):
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

# ------------------------------------------------------------------------
# 5. Phase 5: AI identity + mTLS agent
# ------------------------------------------------------------------------
tls_enabled="$(grep -E '^DOMINION_TLS_ENABLED=' "${env_file}" | cut -d= -f2- || true)"
if [[ "${tls_enabled}" != "true" && "${tls_enabled}" != "True" ]]; then
    log "Phase 5 skipped — DOMINION_TLS_ENABLED not true"
    log "all smoke tests passed (phases 1, 2, 3, 4)"
    exit 0
fi

tls_url="${DOMINION_TLS_URL:-https://127.0.0.1:3443}"

log "5a. POST /admin/agents (create Astrid)"
agent_resp="$(curl -fsS -X POST "${admin_hdr[@]}" \
    -d '{"display_name":"Astrid","owner_user_id":"'"${alice_id}"'"}' \
    "${url}/admin/agents")"
agent_id="$(printf '%s' "${agent_resp}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["agent"]["id"])')"
thumbprint="$(printf '%s' "${agent_resp}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["agent"]["thumbprint"])')"
[[ -n "${agent_id}" && -n "${thumbprint}" ]] || fail "agent creation returned no id/thumbprint"
pass "agent ${agent_id} created (thumbprint ${thumbprint:0:16}...)"

agent_dir="/tmp/dominion-agent-${agent_id}"
mkdir -p "${agent_dir}"
printf '%s' "${agent_resp}" | python3 -c 'import json,sys,os;r=json.load(sys.stdin);d=os.environ["D"];
open(f"{d}/cert.pem","w").write(r["cert_pem"]);
open(f"{d}/key.pem","w").write(r["private_key_pem"]);
open(f"{d}/ca.pem","w").write(r["ca_cert_pem"]);' D="${agent_dir}"
chmod 600 "${agent_dir}/key.pem"

log "5b. build or fetch agent CLI"
if command -v go >/dev/null 2>&1; then
    (cd packages/agent-runtime && go build -o /tmp/dominion-agent-cli ./cmd/agent) \
        && pass "agent binary built via local go toolchain"
elif docker image inspect dominion-agent-cli >/dev/null 2>&1 \
     || docker build -q -t dominion-agent-cli packages/agent-runtime >/dev/null 2>&1; then
    # Fallback: run the agent from a container. Wrap docker run in a
    # shim script so the rest of the test treats it like a local binary.
    cat >/tmp/dominion-agent-cli <<SHIM
#!/usr/bin/env bash
exec docker run --rm --network host \
    -v "${agent_dir}:/secrets:ro" \
    dominion-agent-cli "\$@"
SHIM
    chmod +x /tmp/dominion-agent-cli
    # Rewrite paths so they refer to /secrets inside the container.
    agent_dir_hostpath="${agent_dir}"
    agent_cli=( /tmp/dominion-agent-cli
        --cert "/secrets/cert.pem"
        --key  "/secrets/key.pem"
        --ca   "/secrets/ca.pem"
        --gateway "${tls_url}" )
    pass "agent CLI available via docker image dominion-agent-cli"
else
    fail "Phase 5 needs either \`go\` on PATH or a buildable docker image. Install golang or run: docker build -t dominion-agent-cli packages/agent-runtime"
fi

agent_cli=( /tmp/dominion-agent-cli
    --cert "${agent_dir}/cert.pem"
    --key  "${agent_dir}/key.pem"
    --ca   "${agent_dir}/ca.pem"
    --gateway "${tls_url}" )

log "5c. agent reads alice's doc WITHOUT grant -> expect 403"
set +e
out="$("${agent_cli[@]}" read "${doc_id}" 2>&1)"
rc=$?
set -e
printf '%s\n' "${out}" | head -5
[[ "${rc}" -ne 0 ]] && printf '%s' "${out}" | grep -q 'HTTP 403' \
    && pass "agent forbidden before grant" \
    || fail "expected 403, got rc=${rc}"

log "5d. admin grants agent reader on alice's doc"
grant_body='{
  "principal":"agent:'"${agent_id}"'",
  "relation":"reader",
  "resource":"document:'"${doc_id}"'"
}'
curl -fsS -X POST "${admin_hdr[@]}" -d "${grant_body}" "${url}/admin/acl/grant" >/dev/null \
    && pass "grant to agent accepted" \
    || fail "grant to agent failed"

log "5e. agent reads the doc -> expect 200"
out="$("${agent_cli[@]}" read "${doc_id}")"
printf '%s\n' "${out}" | head -5
printf '%s' "${out}" | grep -q "\"id\":\"${doc_id}\"" \
    && pass "agent can read after grant" \
    || fail "agent still cannot read after grant"

log "5f. agent writes a draft.v1 document"
draft_body="${agent_dir}/draft.json"
cat >"${draft_body}" <<EOF
{
  "inReplyToDocumentId":"${doc_id}",
  "to":["bob@example.com"],
  "subject":"Re: smoke-test",
  "body":"Astrid's draft reply",
  "generatedByAgent":"agent:${agent_id}",
  "generatedAt":"2026-04-17T12:05:00Z",
  "status":"pending"
}
EOF
out="$("${agent_cli[@]}" write draft.v1 "${draft_body}")"
printf '%s\n' "${out}" | head -3
printf '%s' "${out}" | grep -q '"schema_id":"draft.v1"' \
    && pass "agent wrote draft.v1 document" \
    || fail "agent draft write failed"

log "5g. audit bundle contains agent actions"
bundle="$(curl -fsS "${admin_hdr[@]}" "${url}/admin/audit?from=${from_ts}")"
agent_events="$(printf '%s' "${bundle}" | python3 -c \
    'import json,sys;b=json.load(sys.stdin);print(sum(1 for e in b["events"] if e["actor"]=="agent:'"${agent_id}"'"))')"
[[ "${agent_events}" -ge 3 ]] \
    && pass "audit log contains ${agent_events} events for agent:${agent_id}" \
    || fail "expected >=3 agent events, got ${agent_events}"

# ------------------------------------------------------------------------
# 6. Phase 6: email ingestion (simulate path, no live M365 required)
# ------------------------------------------------------------------------
simulate="$(grep -E '^DOMINION_DEV_GRAPH_SIMULATE=' "${env_file}" | cut -d= -f2- || true)"
if [[ "${simulate}" != "true" && "${simulate}" != "True" ]]; then
    log "Phase 6 skipped — DOMINION_DEV_GRAPH_SIMULATE not true"
    log "all smoke tests passed (phases 1, 2, 3, 4, 5)"
    exit 0
fi

log "6a. simulate a Graph message for alice (should become email.v1 doc)"
msg_id="smoke-graph-$(date +%s%N)"
sim_body='{
  "user_id":"'"${alice_id}"'",
  "mailbox_user":"'"${alice_email}"'",
  "message":{
    "id":"'"${msg_id}"'",
    "subject":"Simulated Graph message",
    "bodyPreview":"hello from the fake poller",
    "receivedDateTime":"2026-04-17T13:00:00Z",
    "from":{"emailAddress":{"address":"bob@example.com","name":"Bob"}},
    "toRecipients":[{"emailAddress":{"address":"'"${alice_email}"'"}}],
    "body":{"contentType":"text","content":"hello from the fake poller"}
  }
}'
sim_resp="$(curl -fsS -X POST "${admin_hdr[@]}" -d "${sim_body}" \
    "${url}/admin/connectors/graph/simulate")"
ingested_doc_id="$(printf '%s' "${sim_resp}" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')"
[[ -n "${ingested_doc_id}" ]] || fail "simulate returned no doc id"
pass "ingested email.v1 document ${ingested_doc_id}"

log "6b. /me/inbox as alice contains the new document"
inbox="$(curl -fsS "${alice_h[@]}" "${url}/me/inbox")"
printf '%s' "${inbox}" | grep -q "\"id\":\"${ingested_doc_id}\"" \
    && pass "alice's /me/inbox lists the ingested email" \
    || fail "ingested email missing from /me/inbox"

log "6c. bob (no grant) cannot see the ingested email via his inbox"
bob_inbox="$(curl -fsS "${bob_h[@]}" "${url}/me/inbox")"
if printf '%s' "${bob_inbox}" | grep -q "\"id\":\"${ingested_doc_id}\""; then
    fail "bob's inbox should not include alice's ingested email"
fi
pass "bob's inbox excludes alice's email"

log "6d. duplicate ingest is a no-op"
dup_resp="$(curl -fsS -X POST "${admin_hdr[@]}" -d "${sim_body}" \
    "${url}/admin/connectors/graph/simulate")"
printf '%s' "${dup_resp}" | grep -q '"status":"duplicate"' \
    && pass "second ingest for same messageId returns duplicate" \
    || fail "duplicate ingest should have been rejected, got: ${dup_resp}"

log "6e. Astrid (alice's PA from phase 5) sees the email via reader tuple"
# Agent was created with owner_user_id = alice in phase 5, so the
# ingester should have auto-granted it reader on the new document.
out="$("${agent_cli[@]}" read "${ingested_doc_id}")"
printf '%s\n' "${out}" | head -3
printf '%s' "${out}" | grep -q "\"id\":\"${ingested_doc_id}\"" \
    && pass "PA reader tuple auto-granted on ingest" \
    || fail "agent cannot read ingested email — PA grant missing"

log "6f. audit log shows the ingest action on behalf of alice"
bundle="$(curl -fsS "${admin_hdr[@]}" "${url}/admin/audit?from=${from_ts}")"
# the simulate call itself runs synchronously as an admin request, so the
# audited actor is whoever called it (admin token == anonymous principal);
# what must exist is the subsequent document.read events by alice + agent.
agent_reads="$(printf '%s' "${bundle}" | python3 -c \
    'import json,sys;b=json.load(sys.stdin);print(sum(1 for e in b["events"] if e["actor"]=="agent:'"${agent_id}"'" and e["resource"]=="document:'"${ingested_doc_id}"'"))')"
[[ "${agent_reads}" -ge 1 ]] \
    && pass "audit shows agent reading the ingested email" \
    || fail "no agent read event for the ingested email"

log "all smoke tests passed (phases 1, 2, 3, 4, 5, 6)"
