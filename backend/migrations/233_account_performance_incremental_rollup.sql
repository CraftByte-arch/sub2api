-- Incremental account-performance hourly rollup queue.
--
-- Each minute-table write marks only the affected UTC hour as dirty. The
-- background rollup claims closed dirty hours and therefore never needs to
-- rescan the complete retained minute table.

CREATE TABLE IF NOT EXISTS account_performance_rollup_dirty_hours (
    bucket_start TIMESTAMPTZ PRIMARY KEY
);

COMMENT ON TABLE account_performance_rollup_dirty_hours IS
    'Closed UTC hours that must be rebuilt from account_performance_minute.';

CREATE OR REPLACE FUNCTION mark_account_performance_rollup_dirty_hours()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO account_performance_rollup_dirty_hours (bucket_start)
    SELECT DISTINCT date_trunc('hour', bucket_start)
    FROM account_performance_rollup_changed_rows
    ON CONFLICT (bucket_start)
    DO NOTHING;

    RETURN NULL;
END;
$$;

-- Separate statement triggers are required so INSERT ... ON CONFLICT DO UPDATE
-- marks both newly inserted minute rows and rows whose counters were updated.
DROP TRIGGER IF EXISTS account_performance_rollup_dirty_after_insert
    ON account_performance_minute;
CREATE TRIGGER account_performance_rollup_dirty_after_insert
AFTER INSERT ON account_performance_minute
REFERENCING NEW TABLE AS account_performance_rollup_changed_rows
FOR EACH STATEMENT
EXECUTE FUNCTION mark_account_performance_rollup_dirty_hours();

DROP TRIGGER IF EXISTS account_performance_rollup_dirty_after_update
    ON account_performance_minute;
CREATE TRIGGER account_performance_rollup_dirty_after_update
AFTER UPDATE ON account_performance_minute
REFERENCING NEW TABLE AS account_performance_rollup_changed_rows
FOR EACH STATEMENT
EXECUTE FUNCTION mark_account_performance_rollup_dirty_hours();

-- Seed every retained source hour once so deployments can repair hours missed
-- by a previously timed-out legacy rollup. This is the only full minute-table
-- scan; subsequent work is driven entirely by the dirty-hour queue.
SET LOCAL max_parallel_workers_per_gather = 0;

INSERT INTO account_performance_rollup_dirty_hours (bucket_start)
SELECT DISTINCT date_trunc('hour', bucket_start)
FROM account_performance_minute
ON CONFLICT (bucket_start)
DO NOTHING;

-- Down migration (documentation only):
-- DROP TRIGGER IF EXISTS account_performance_rollup_dirty_after_insert ON account_performance_minute;
-- DROP TRIGGER IF EXISTS account_performance_rollup_dirty_after_update ON account_performance_minute;
-- DROP FUNCTION IF EXISTS mark_account_performance_rollup_dirty_hours();
-- DROP TABLE IF EXISTS account_performance_rollup_dirty_hours;
