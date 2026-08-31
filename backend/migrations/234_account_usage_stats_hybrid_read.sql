-- Stage one of moving Account usage statistics off the usage_logs write path.
--
-- This migration intentionally keeps the synchronous Account delta triggers
-- installed. The application first learns to read closed aggregate history
-- together with the raw open tail; trigger removal is a separate deployment.

ALTER TABLE account_usage_stats_daily_state
    ADD COLUMN IF NOT EXISTS closed_before DATE NULL;

COMMENT ON COLUMN account_usage_stats_daily_state.closed_before IS
    'Every bucket_date before this date is closed and may be read from the daily aggregate.';

-- Existing ready installations have already rebuilt and verified their retained
-- range. The current date stays open and is therefore always read from usage_logs.
UPDATE account_usage_stats_daily_state
SET closed_before = LEAST(COALESCE(cursor, CURRENT_DATE), CURRENT_DATE),
    updated_at = NOW()
WHERE id = 1
  AND ready = TRUE
  AND coverage_start IS NOT NULL
  AND closed_before IS NULL;

CREATE TABLE IF NOT EXISTS account_usage_stats_dirty_days (
    bucket_date DATE PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE account_usage_stats_dirty_days IS
    'Closed Account-stat dates that must be read raw and rebuilt before aggregate reuse.';

-- Stage two (a later forward migration) will replace only these Account custom
-- triggers with lightweight historical dirty-day markers:
--   trg_account_usage_stats_daily_insert
--   trg_account_usage_stats_daily_delete
-- The stage-one migration must not drop or replace them.
