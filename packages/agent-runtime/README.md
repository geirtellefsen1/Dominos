# agent-runtime

Placeholder. In Phase 5 this directory becomes a fork of
[github.com/openclaw/openclaw](https://github.com/openclaw/openclaw) with the
governance wrapper from spec §3.5:

- Every agent presents an X.509 cert issued by the Dominion internal CA.
- Every tool call routes through the api-gateway — no direct DB / FGA / Graph.
- Every invocation emits audit events.
- No autonomous outbound actions in v0; agents propose, humans approve.

Do not import OpenClaw upstream until Phase 5 begins.
