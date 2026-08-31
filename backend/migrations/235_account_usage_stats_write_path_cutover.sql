-- Stage two of moving Account usage statistics off the usage_logs write path.
--
-- The stage-one application is already able to combine closed aggregate days
-- with raw open/dirty days. This migration therefore removes only the custom
-- Account delta triggers and replaces them with historical dirty-day markers.
-- Other custom and upstream usage_logs triggers remain untouched.

-- Never let the standby migration queue normal usage writes for an unbounded
-- period. Failure rolls this transaction back and leaves the active stage-one
-- slot serving with the original triggers intact.
SET LOCAL lock_timeout = '1s';
SET LOCAL statement_timeout = '10s';

CREATE OR REPLACE FUNCTION mark_account_usage_stats_dirty_days_after_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    affected_date DATE;
    published_before DATE;
BEGIN
    SELECT MIN(rows.created_at::DATE)
    INTO affected_date
    FROM account_usage_stats_new_rows rows
    WHERE rows.account_id > 0;

    -- CURRENT_DATE is always the open application-timezone bucket. Keep the
    -- normal usage INSERT path free of state-row locks and persistent writes.
    IF affected_date IS NULL OR affected_date >= CURRENT_DATE THEN
        RETURN NULL;
    END IF;

    -- Serialize historical writes with a concurrent closed-day repair. If the
    -- repair advances closed_before first, this trigger observes the new value
    -- and marks the just-repaired day dirty before the usage mutation commits.
    SELECT closed_before
    INTO published_before
    FROM account_usage_stats_daily_state
    WHERE id = 1 AND ready = TRUE
    FOR KEY SHARE;

    IF published_before IS NULL OR affected_date >= published_before THEN
        RETURN NULL;
    END IF;

    INSERT INTO account_usage_stats_dirty_days (bucket_date)
    SELECT DISTINCT rows.created_at::DATE
    FROM account_usage_stats_new_rows rows
    WHERE rows.account_id > 0
      AND rows.created_at::DATE < published_before
    ON CONFLICT (bucket_date) DO NOTHING;

    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION mark_account_usage_stats_dirty_days_after_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    affected_date DATE;
    published_before DATE;
BEGIN
    SELECT MIN(rows.created_at::DATE)
    INTO affected_date
    FROM account_usage_stats_old_rows rows
    WHERE rows.account_id > 0;

    IF affected_date IS NULL OR affected_date >= CURRENT_DATE THEN
        RETURN NULL;
    END IF;

    SELECT closed_before
    INTO published_before
    FROM account_usage_stats_daily_state
    WHERE id = 1 AND ready = TRUE
    FOR KEY SHARE;

    IF published_before IS NULL OR affected_date >= published_before THEN
        RETURN NULL;
    END IF;

    INSERT INTO account_usage_stats_dirty_days (bucket_date)
    SELECT DISTINCT rows.created_at::DATE
    FROM account_usage_stats_old_rows rows
    WHERE rows.account_id > 0
      AND rows.created_at::DATE < published_before
    ON CONFLICT (bucket_date) DO NOTHING;

    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION mark_account_usage_stats_dirty_days_after_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    affected_date DATE;
    published_before DATE;
BEGIN
    SELECT MIN(changed.bucket_date)
    INTO affected_date
    FROM (
        SELECT rows.created_at::DATE AS bucket_date
        FROM account_usage_stats_old_rows rows
        WHERE rows.account_id > 0

        UNION ALL

        SELECT rows.created_at::DATE AS bucket_date
        FROM account_usage_stats_new_rows rows
        WHERE rows.account_id > 0
    ) changed;

    IF affected_date IS NULL OR affected_date >= CURRENT_DATE THEN
        RETURN NULL;
    END IF;

    SELECT closed_before
    INTO published_before
    FROM account_usage_stats_daily_state
    WHERE id = 1 AND ready = TRUE
    FOR KEY SHARE;

    IF published_before IS NULL OR affected_date >= published_before THEN
        RETURN NULL;
    END IF;

    INSERT INTO account_usage_stats_dirty_days (bucket_date)
    SELECT DISTINCT changed.bucket_date
    FROM (
        SELECT rows.created_at::DATE AS bucket_date
        FROM account_usage_stats_old_rows rows
        WHERE rows.account_id > 0

        UNION ALL

        SELECT rows.created_at::DATE AS bucket_date
        FROM account_usage_stats_new_rows rows
        WHERE rows.account_id > 0
    ) changed
    WHERE changed.bucket_date < published_before
    ON CONFLICT (bucket_date) DO NOTHING;

    RETURN NULL;
END;
$$;

-- Function definitions above do not lock usage_logs. Keep the AccessExclusive
-- trigger replacement at the very end so the lock-holding transaction is as
-- short as possible.
DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_insert ON usage_logs;
DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_delete ON usage_logs;
DROP FUNCTION IF EXISTS apply_account_usage_stats_daily_delta();

DROP TRIGGER IF EXISTS trg_account_usage_stats_dirty_insert ON usage_logs;
CREATE TRIGGER trg_account_usage_stats_dirty_insert
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS account_usage_stats_new_rows
FOR EACH STATEMENT
EXECUTE FUNCTION mark_account_usage_stats_dirty_days_after_insert();

DROP TRIGGER IF EXISTS trg_account_usage_stats_dirty_delete ON usage_logs;
CREATE TRIGGER trg_account_usage_stats_dirty_delete
AFTER DELETE ON usage_logs
REFERENCING OLD TABLE AS account_usage_stats_old_rows
FOR EACH STATEMENT
EXECUTE FUNCTION mark_account_usage_stats_dirty_days_after_delete();

DROP TRIGGER IF EXISTS trg_account_usage_stats_dirty_update ON usage_logs;
CREATE TRIGGER trg_account_usage_stats_dirty_update
AFTER UPDATE ON usage_logs
REFERENCING OLD TABLE AS account_usage_stats_old_rows
            NEW TABLE AS account_usage_stats_new_rows
FOR EACH STATEMENT
EXECUTE FUNCTION mark_account_usage_stats_dirty_days_after_update();
