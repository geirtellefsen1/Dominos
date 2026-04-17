# agent-runtime

The Dominion agent runtime. Phase 5 ships the minimum viable version:
a standalone Go binary (`cmd/agent`) that authenticates to the gateway
with a client certificate signed by the Dominion internal CA and calls
document tools through the gateway's mTLS listener.

This is **not yet** a full fork of [openclaw/openclaw](https://github.com/openclaw/openclaw).
Spec §3.5 prescribes that fork; the work lands incrementally as the
higher phases need features OpenClaw already has (scheduled tasks,
tool adapters, LLM calls). What matters for Phase 5 is that every agent
tool call already flows through the gateway, subject to the same auth,
ACL, and audit middleware as humans — that's the governance claim.

## What the binary does

```
agent read <doc-uuid>               # GET  /documents/{id}
agent list [--schema <schema-id>]   # GET  /documents?schema=...
agent write <schema-id> <body.json> # POST /documents
agent whoami                        # harmless GET to trace your principal
```

Required flags on every invocation:

```
--cert    path to agent certificate PEM (returned by POST /admin/agents)
--key     path to matching private key PEM
--ca      path to Dominion CA cert PEM
--gateway https://gateway-host:3443
```

## Obtaining credentials

Ask the admin for a new agent:

```sh
curl -X POST -H "Authorization: Bearer $DOMINION_ADMIN_TOKEN" \
  -H 'content-type: application/json' \
  -d '{"display_name":"Astrid","owner_user_id":"<user-uuid>"}' \
  https://gateway-host:3000/admin/agents
```

The response includes `cert_pem`, `private_key_pem`, and `ca_cert_pem`
— save all three. The private key is returned **once**; the gateway
never stores it.

## Building

```sh
go build -o bin/agent ./cmd/agent
# or
docker build -t dominion-agent-cli .
```

## Non-goals for v0

- No `send_email` tool. Phase 5 agents can propose (write `draft.v1`
  documents) but cannot send; humans approve and send via Phase 7's
  approval queue.
- No LLM integration — that starts in Phase 7.
- No OpenClaw upstream features (canvas, voice, platform adapters).
