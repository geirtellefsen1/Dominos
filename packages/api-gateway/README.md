# api-gateway

Single entry point for all client traffic. Every request flows through three
middlewares in order: auth → ACL check → audit tap (spec §3.1).

Phase 0: `/health` only. Real middleware chain lands in phases 2–4.

## Run

```sh
go run .
# or from repo root: make gateway
```

Defaults to `:3000`; override with `DOMINION_GATEWAY_ADDR`.
