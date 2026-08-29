-- Durable per-user daily route aggregates for the ordinary user dashboard.
-- The route dimensions remain unresolved so platform changes on groups/accounts
-- are reflected when the dashboard is read instead of being frozen at write time.
CREATE TABLE IF NOT EXISTS user_dashboard_route_daily (
    user_id BIGINT NOT NULL,
    bucket_date DATE NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    account_id BIGINT NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost NUMERIC NOT NULL DEFAULT 0,
    actual_cost NUMERIC NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    billable_requests BIGINT NOT NULL DEFAULT 0,
    billable_tokens BIGINT NOT NULL DEFAULT 0,
    billable_actual_cost NUMERIC NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, bucket_date, group_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_user_dashboard_route_daily_bucket_date
    ON user_dashboard_route_daily (bucket_date);

-- A single durable row controls resumable backfill and read cutover.
CREATE TABLE IF NOT EXISTS user_dashboard_route_daily_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    ready BOOLEAN NOT NULL DEFAULT FALSE,
    coverage_start DATE NULL,
    cursor DATE NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO user_dashboard_route_daily_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- Every trigger exposes its transition relation under the same name and passes
-- the sign as TG_ARGV[0]. Each statement is grouped before touching the compact
-- aggregate table, avoiding one aggregate upsert per raw usage row.
CREATE OR REPLACE FUNCTION apply_user_dashboard_route_daily_delta()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    delta BIGINT := TG_ARGV[0]::BIGINT;
BEGIN
    INSERT INTO user_dashboard_route_daily (
        user_id,
        bucket_date,
        group_id,
        account_id,
        requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        total_cost,
        actual_cost,
        duration_sum_ms,
        duration_count,
        billable_requests,
        billable_tokens,
        billable_actual_cost,
        updated_at
    )
    SELECT
        delta_rows.user_id,
        delta_rows.created_at::DATE,
        COALESCE(delta_rows.group_id, 0),
        delta_rows.account_id,
        delta * COUNT(*)::BIGINT,
        delta * COALESCE(SUM(delta_rows.input_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(delta_rows.output_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(delta_rows.cache_creation_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(delta_rows.cache_read_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(delta_rows.total_cost), 0),
        delta * COALESCE(SUM(delta_rows.actual_cost), 0),
        delta * COALESCE(SUM(COALESCE(delta_rows.duration_ms, 0)), 0)::BIGINT,
        delta * COUNT(delta_rows.duration_ms)::BIGINT,
        delta * (COUNT(*) FILTER (WHERE delta_rows.actual_cost > 0))::BIGINT,
        delta * COALESCE(SUM(
            delta_rows.input_tokens::BIGINT
            + delta_rows.output_tokens::BIGINT
            + delta_rows.cache_creation_tokens::BIGINT
            + delta_rows.cache_read_tokens::BIGINT
        ) FILTER (WHERE delta_rows.actual_cost > 0), 0)::BIGINT,
        delta * COALESCE(SUM(delta_rows.actual_cost) FILTER (WHERE delta_rows.actual_cost > 0), 0),
        NOW()
    FROM user_dashboard_delta_rows delta_rows
    WHERE delta_rows.user_id > 0
        AND delta_rows.account_id > 0
    GROUP BY
        delta_rows.user_id,
        delta_rows.created_at::DATE,
        COALESCE(delta_rows.group_id, 0),
        delta_rows.account_id
    ON CONFLICT (user_id, bucket_date, group_id, account_id)
    DO UPDATE SET
        requests = user_dashboard_route_daily.requests + EXCLUDED.requests,
        input_tokens = user_dashboard_route_daily.input_tokens + EXCLUDED.input_tokens,
        output_tokens = user_dashboard_route_daily.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = user_dashboard_route_daily.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = user_dashboard_route_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = user_dashboard_route_daily.total_cost + EXCLUDED.total_cost,
        actual_cost = user_dashboard_route_daily.actual_cost + EXCLUDED.actual_cost,
        duration_sum_ms = user_dashboard_route_daily.duration_sum_ms + EXCLUDED.duration_sum_ms,
        duration_count = user_dashboard_route_daily.duration_count + EXCLUDED.duration_count,
        billable_requests = user_dashboard_route_daily.billable_requests + EXCLUDED.billable_requests,
        billable_tokens = user_dashboard_route_daily.billable_tokens + EXCLUDED.billable_tokens,
        billable_actual_cost = user_dashboard_route_daily.billable_actual_cost + EXCLUDED.billable_actual_cost,
        updated_at = NOW();

    DELETE FROM user_dashboard_route_daily target
    USING (
        SELECT DISTINCT
            delta_rows.user_id,
            delta_rows.created_at::DATE AS bucket_date,
            COALESCE(delta_rows.group_id, 0) AS group_id,
            delta_rows.account_id
        FROM user_dashboard_delta_rows delta_rows
        WHERE delta_rows.user_id > 0
            AND delta_rows.account_id > 0
    ) changed
    WHERE target.user_id = changed.user_id
        AND target.bucket_date = changed.bucket_date
        AND target.group_id = changed.group_id
        AND target.account_id = changed.account_id
        AND target.requests <= 0;

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_dashboard_route_daily_insert ON usage_logs;
CREATE TRIGGER trg_user_dashboard_route_daily_insert
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS user_dashboard_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_dashboard_route_daily_delta('1');

DROP TRIGGER IF EXISTS trg_user_dashboard_route_daily_delete ON usage_logs;
CREATE TRIGGER trg_user_dashboard_route_daily_delete
AFTER DELETE ON usage_logs
REFERENCING OLD TABLE AS user_dashboard_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_dashboard_route_daily_delta('-1');

-- UPDATE is intentionally handled by two statement triggers: remove the old
-- contribution first (trigger names sort before the add trigger), then add the
-- new contribution. Runtime usage logs are normally immutable, but this keeps
-- administrative corrections and future migrations consistent.
DROP TRIGGER IF EXISTS trg_user_dashboard_route_daily_update_1_old ON usage_logs;
CREATE TRIGGER trg_user_dashboard_route_daily_update_1_old
AFTER UPDATE ON usage_logs
REFERENCING OLD TABLE AS user_dashboard_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_dashboard_route_daily_delta('-1');

DROP TRIGGER IF EXISTS trg_user_dashboard_route_daily_update_2_new ON usage_logs;
CREATE TRIGGER trg_user_dashboard_route_daily_update_2_new
AFTER UPDATE ON usage_logs
REFERENCING NEW TABLE AS user_dashboard_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_dashboard_route_daily_delta('1');
