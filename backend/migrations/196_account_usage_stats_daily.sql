-- Durable daily account statistics used by the administrator account details.
-- The database session timezone is configured from the application timezone,
-- so bucket_date matches the legacy created_at::date reporting boundary.
CREATE TABLE IF NOT EXISTS account_usage_stats_daily (
    account_id BIGINT NOT NULL,
    bucket_date DATE NOT NULL,
    dimension_type SMALLINT NOT NULL CHECK (dimension_type BETWEEN 0 AND 3),
    dimension_value TEXT NOT NULL DEFAULT '',
    requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    standard_cost NUMERIC NOT NULL DEFAULT 0,
    account_cost NUMERIC NOT NULL DEFAULT 0,
    user_cost NUMERIC NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, bucket_date, dimension_type, dimension_value)
);

CREATE INDEX IF NOT EXISTS idx_account_usage_stats_daily_bucket_date
    ON account_usage_stats_daily (bucket_date);

-- A single durable row controls the resumable 90-day backfill and read cutover.
CREATE TABLE IF NOT EXISTS account_usage_stats_daily_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    ready BOOLEAN NOT NULL DEFAULT FALSE,
    coverage_start DATE NULL,
    cursor DATE NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO account_usage_stats_daily_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- Both triggers expose their transition relation under the same name. This
-- covers every usage-log persistence path without coupling its INSERT SQL to
-- the aggregate implementation. Only rows actually inserted by an idempotent
-- INSERT ... ON CONFLICT statement appear in the transition relation.
CREATE OR REPLACE FUNCTION apply_account_usage_stats_daily_delta()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    delta BIGINT := CASE WHEN TG_OP = 'DELETE' THEN -1 ELSE 1 END;
BEGIN
    INSERT INTO account_usage_stats_daily (
        account_id,
        bucket_date,
        dimension_type,
        dimension_value,
        requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        standard_cost,
        account_cost,
        user_cost,
        duration_sum_ms,
        duration_count,
        updated_at
    )
    SELECT
        expanded.account_id,
        expanded.bucket_date,
        expanded.dimension_type,
        expanded.dimension_value,
        delta * COUNT(*)::BIGINT,
        delta * SUM(expanded.input_tokens)::BIGINT,
        delta * SUM(expanded.output_tokens)::BIGINT,
        delta * SUM(expanded.cache_creation_tokens)::BIGINT,
        delta * SUM(expanded.cache_read_tokens)::BIGINT,
        delta * SUM(expanded.total_cost),
        delta * SUM(expanded.account_cost),
        delta * SUM(expanded.actual_cost),
        delta * SUM(COALESCE(expanded.duration_ms, 0))::BIGINT,
        delta * COUNT(expanded.duration_ms)::BIGINT,
        NOW()
    FROM (
        SELECT
            ul.account_id,
            ul.created_at::DATE AS bucket_date,
            dimensions.dimension_type,
            dimensions.dimension_value,
            ul.input_tokens,
            ul.output_tokens,
            ul.cache_creation_tokens,
            ul.cache_read_tokens,
            ul.total_cost,
            COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1) AS account_cost,
            ul.actual_cost,
            ul.duration_ms
        FROM account_usage_stats_delta_rows ul
        CROSS JOIN LATERAL (
            VALUES
                (0::SMALLINT, ''::TEXT),
                (1::SMALLINT, COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)),
                (2::SMALLINT, COALESCE(NULLIF(TRIM(ul.inbound_endpoint), ''), 'unknown')),
                (3::SMALLINT, COALESCE(NULLIF(TRIM(ul.upstream_endpoint), ''), 'unknown'))
        ) AS dimensions(dimension_type, dimension_value)
        WHERE ul.account_id > 0
    ) expanded
    GROUP BY
        expanded.account_id,
        expanded.bucket_date,
        expanded.dimension_type,
        expanded.dimension_value
    ON CONFLICT (account_id, bucket_date, dimension_type, dimension_value)
    DO UPDATE SET
        requests = account_usage_stats_daily.requests + EXCLUDED.requests,
        input_tokens = account_usage_stats_daily.input_tokens + EXCLUDED.input_tokens,
        output_tokens = account_usage_stats_daily.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = account_usage_stats_daily.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = account_usage_stats_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
        standard_cost = account_usage_stats_daily.standard_cost + EXCLUDED.standard_cost,
        account_cost = account_usage_stats_daily.account_cost + EXCLUDED.account_cost,
        user_cost = account_usage_stats_daily.user_cost + EXCLUDED.user_cost,
        duration_sum_ms = account_usage_stats_daily.duration_sum_ms + EXCLUDED.duration_sum_ms,
        duration_count = account_usage_stats_daily.duration_count + EXCLUDED.duration_count,
        updated_at = NOW();

    -- Keep the table compact when the final raw log for a dimension is removed.
    DELETE FROM account_usage_stats_daily target
    USING (
        SELECT DISTINCT
            ul.account_id,
            ul.created_at::DATE AS bucket_date,
            dimensions.dimension_type,
            dimensions.dimension_value
        FROM account_usage_stats_delta_rows ul
        CROSS JOIN LATERAL (
            VALUES
                (0::SMALLINT, ''::TEXT),
                (1::SMALLINT, COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)),
                (2::SMALLINT, COALESCE(NULLIF(TRIM(ul.inbound_endpoint), ''), 'unknown')),
                (3::SMALLINT, COALESCE(NULLIF(TRIM(ul.upstream_endpoint), ''), 'unknown'))
        ) AS dimensions(dimension_type, dimension_value)
        WHERE ul.account_id > 0
    ) changed
    WHERE target.account_id = changed.account_id
        AND target.bucket_date = changed.bucket_date
        AND target.dimension_type = changed.dimension_type
        AND target.dimension_value = changed.dimension_value
        AND target.requests <= 0;

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_insert ON usage_logs;
CREATE TRIGGER trg_account_usage_stats_daily_insert
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS account_usage_stats_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_account_usage_stats_daily_delta();

DROP TRIGGER IF EXISTS trg_account_usage_stats_daily_delete ON usage_logs;
CREATE TRIGGER trg_account_usage_stats_daily_delete
AFTER DELETE ON usage_logs
REFERENCING OLD TABLE AS account_usage_stats_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_account_usage_stats_daily_delta();
