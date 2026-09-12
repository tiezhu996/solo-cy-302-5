-- Migration 0003: make proctor event deduplication safe under concurrency.
-- Reports of the same attempt and type within one 30-second window bucket are
-- guarded by a unique index, so simultaneous inserts can never duplicate.

ALTER TABLE proctor_events ADD COLUMN window_bucket BIGINT NOT NULL DEFAULT 0;

-- Backfill the bucket for existing rows from their occurrence time.
UPDATE proctor_events SET window_bucket = FLOOR(UNIX_TIMESTAMP(occurred_at) / 30);

-- Collapse pre-existing duplicates inside the same bucket, keeping the first.
DELETE e1 FROM proctor_events e1
JOIN proctor_events e2
  ON e1.attempt_id = e2.attempt_id
 AND e1.type = e2.type
 AND e1.window_bucket = e2.window_bucket
 AND e1.id > e2.id;

ALTER TABLE proctor_events ADD UNIQUE KEY uk_proctor_dedup (attempt_id, type, window_bucket);
