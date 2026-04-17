# email-worker

Placeholder. Phase 6: Microsoft Graph polling worker (spec §3.6).

Holds per-user OAuth refresh tokens, polls `/me/messages` every 60s, writes
each new message as an `email.v1` document owned by the user with the user's
PA granted `reader`.
