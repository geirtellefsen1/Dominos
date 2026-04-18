-- Add `sending` and `error` to the draft.v1 status enum so the queue
-- approve path can atomically transition:
--
--    pending  -> sending -> sent           (Graph accepted the mail)
--    pending  -> sending -> pending        (retryable Graph error)
--    pending  -> sending -> error          (unretryable failure)
--
-- Before Sprint 1 #5 the enum was ["pending","approved","rejected","sent"]
-- and the approve path sent-then-updated, which meant a DB blip between
-- the two steps could send the same email twice on retry.
--
-- Idempotent under re-run: jsonb_set replaces the full enum array.

UPDATE schemas
SET json_schema = jsonb_set(
    json_schema,
    '{properties,status,enum}',
    '["pending","sending","approved","rejected","sent","error"]'::jsonb
)
WHERE id = 'draft.v1';
