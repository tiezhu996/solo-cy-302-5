-- Migration 0003: make proctor event deduplication safe under concurrency.
-- Reports of the same attempt and type within one 30-second window bucket are
-- guarded by a unique index, so simultaneous inserts can never duplicate.
--
-- NOTE: the application performs this migration automatically at startup
-- (Repository.AutoMigrate runs the legacy-data cleanup before GORM creates
-- the index). This file documents the canonical SQL for manual upgrades.

ALTER TABLE proctor_events ADD COLUMN window_bucket BIGINT NOT NULL DEFAULT 0;

-- Backfill the bucket for existing rows from their occurrence time.
UPDATE proctor_events SET window_bucket = FLOOR(UNIX_TIMESTAMP(occurred_at) / 30);

-- Collapse pre-existing duplicates inside the same bucket. A reviewed event
-- (status <> 'pending') is kept so its conclusion is not lost; otherwise the
-- earliest report wins.
DELETE e FROM proctor_events e
JOIN (
    SELECT attempt_id, type, window_bucket,
           COALESCE(MIN(CASE WHEN status <> 'pending' THEN id END), MIN(id)) AS keep_id
    FROM proctor_events
    GROUP BY attempt_id, type, window_bucket
    HAVING COUNT(*) > 1
) k
  ON e.attempt_id = k.attempt_id
 AND e.type = k.type
 AND e.window_bucket = k.window_bucket
WHERE e.id <> k.keep_id;

ALTER TABLE proctor_events ADD UNIQUE KEY uk_proctor_dedup (attempt_id, type, window_bucket);
