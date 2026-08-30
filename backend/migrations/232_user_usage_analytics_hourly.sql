-- Exact hourly aggregates for ordinary-user usage-page filters.
-- Reads remain disabled by the state row until historical backfill verifies
-- every covered segment against usage_logs.
CREATE TABLE IF NOT EXISTS user_usage_analytics_hourly (
    user_id BIGINT NOT NULL,
    bucket_start TIMESTAMPTZ NOT NULL,
    api_key_id BIGINT NOT NULL DEFAULT 0,
    group_id BIGINT NOT NULL DEFAULT 0,
    requested_model VARCHAR(100) NOT NULL,
    request_type SMALLINT NOT NULL DEFAULT 0,
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    openai_ws_mode BOOLEAN NOT NULL DEFAULT FALSE,
    billing_type SMALLINT NOT NULL DEFAULT 0,
    billing_mode VARCHAR(20) NOT NULL,
    inbound_endpoint VARCHAR(128) NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost NUMERIC NOT NULL DEFAULT 0,
    actual_cost NUMERIC NOT NULL DEFAULT 0,
    account_cost NUMERIC NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (
        user_id,
        bucket_start,
        api_key_id,
        group_id,
        requested_model,
        request_type,
        stream,
        openai_ws_mode,
        billing_type,
        billing_mode,
        inbound_endpoint
    )
);

CREATE INDEX IF NOT EXISTS idx_user_usage_analytics_hourly_bucket_start
    ON user_usage_analytics_hourly (bucket_start);

CREATE TABLE IF NOT EXISTS user_usage_analytics_hourly_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    ready BOOLEAN NOT NULL DEFAULT FALSE,
    coverage_start TIMESTAMPTZ NULL,
    cursor TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO user_usage_analytics_hourly_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

-- All statement triggers expose their transition relation under the same
-- name and pass +1/-1 as TG_ARGV[0]. Normalization is deliberately identical
-- to the legacy user usage filters:
--   * requested model falls back to usage_logs.model;
--   * request_type keeps the raw legacy value, with stream/openai_ws_mode as
--     separate dimensions so request_type=0 compatibility remains exact;
--   * empty billing_mode falls back to image/token using image_count;
--   * blank inbound endpoints become "unknown".
CREATE OR REPLACE FUNCTION apply_user_usage_analytics_hourly_delta()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    delta BIGINT := TG_ARGV[0]::BIGINT;
BEGIN
    WITH normalized AS (
        SELECT
            rows.user_id,
            (DATE_TRUNC('hour', rows.created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS bucket_start,
            COALESCE(rows.api_key_id, 0) AS api_key_id,
            COALESCE(rows.group_id, 0) AS group_id,
            COALESCE(NULLIF(TRIM(rows.requested_model), ''), rows.model) AS requested_model,
            CASE WHEN rows.request_type BETWEEN 0 AND 5 THEN rows.request_type ELSE 0 END AS request_type,
            COALESCE(rows.stream, FALSE) AS stream,
            COALESCE(rows.openai_ws_mode, FALSE) AS openai_ws_mode,
            COALESCE(rows.billing_type, 0)::SMALLINT AS billing_type,
            CASE
                WHEN rows.billing_mode IS NULL OR rows.billing_mode = '' THEN
                    CASE WHEN COALESCE(rows.image_count, 0) > 0 THEN 'image' ELSE 'token' END
                ELSE rows.billing_mode
            END AS billing_mode,
            COALESCE(NULLIF(TRIM(rows.inbound_endpoint), ''), 'unknown') AS inbound_endpoint,
            rows.input_tokens,
            rows.output_tokens,
            rows.cache_creation_tokens,
            rows.cache_read_tokens,
            rows.total_cost,
            rows.actual_cost,
            COALESCE(rows.account_stats_cost, rows.total_cost) * COALESCE(rows.account_rate_multiplier, 1) AS account_cost,
            rows.duration_ms
        FROM user_usage_analytics_delta_rows rows
        WHERE rows.user_id > 0
    )
    INSERT INTO user_usage_analytics_hourly (
        user_id,
        bucket_start,
        api_key_id,
        group_id,
        requested_model,
        request_type,
        stream,
        openai_ws_mode,
        billing_type,
        billing_mode,
        inbound_endpoint,
        requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        total_cost,
        actual_cost,
        account_cost,
        duration_sum_ms,
        duration_count,
        updated_at
    )
    SELECT
        normalized.user_id,
        normalized.bucket_start,
        normalized.api_key_id,
        normalized.group_id,
        normalized.requested_model,
        normalized.request_type,
        normalized.stream,
        normalized.openai_ws_mode,
        normalized.billing_type,
        normalized.billing_mode,
        normalized.inbound_endpoint,
        delta * COUNT(*)::BIGINT,
        delta * COALESCE(SUM(normalized.input_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(normalized.output_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(normalized.cache_creation_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(normalized.cache_read_tokens), 0)::BIGINT,
        delta * COALESCE(SUM(normalized.total_cost), 0),
        delta * COALESCE(SUM(normalized.actual_cost), 0),
        delta * COALESCE(SUM(normalized.account_cost), 0),
        delta * COALESCE(SUM(COALESCE(normalized.duration_ms, 0)), 0)::BIGINT,
        delta * COUNT(normalized.duration_ms)::BIGINT,
        NOW()
    FROM normalized
    GROUP BY
        normalized.user_id,
        normalized.bucket_start,
        normalized.api_key_id,
        normalized.group_id,
        normalized.requested_model,
        normalized.request_type,
        normalized.stream,
        normalized.openai_ws_mode,
        normalized.billing_type,
        normalized.billing_mode,
        normalized.inbound_endpoint
    ON CONFLICT (
        user_id,
        bucket_start,
        api_key_id,
        group_id,
        requested_model,
        request_type,
        stream,
        openai_ws_mode,
        billing_type,
        billing_mode,
        inbound_endpoint
    )
    DO UPDATE SET
        requests = user_usage_analytics_hourly.requests + EXCLUDED.requests,
        input_tokens = user_usage_analytics_hourly.input_tokens + EXCLUDED.input_tokens,
        output_tokens = user_usage_analytics_hourly.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = user_usage_analytics_hourly.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = user_usage_analytics_hourly.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = user_usage_analytics_hourly.total_cost + EXCLUDED.total_cost,
        actual_cost = user_usage_analytics_hourly.actual_cost + EXCLUDED.actual_cost,
        account_cost = user_usage_analytics_hourly.account_cost + EXCLUDED.account_cost,
        duration_sum_ms = user_usage_analytics_hourly.duration_sum_ms + EXCLUDED.duration_sum_ms,
        duration_count = user_usage_analytics_hourly.duration_count + EXCLUDED.duration_count,
        updated_at = NOW();

    DELETE FROM user_usage_analytics_hourly target
    USING (
        SELECT DISTINCT
            rows.user_id,
            (DATE_TRUNC('hour', rows.created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS bucket_start,
            COALESCE(rows.api_key_id, 0) AS api_key_id,
            COALESCE(rows.group_id, 0) AS group_id,
            COALESCE(NULLIF(TRIM(rows.requested_model), ''), rows.model) AS requested_model,
            CASE WHEN rows.request_type BETWEEN 0 AND 5 THEN rows.request_type ELSE 0 END AS request_type,
            COALESCE(rows.stream, FALSE) AS stream,
            COALESCE(rows.openai_ws_mode, FALSE) AS openai_ws_mode,
            COALESCE(rows.billing_type, 0)::SMALLINT AS billing_type,
            CASE
                WHEN rows.billing_mode IS NULL OR rows.billing_mode = '' THEN
                    CASE WHEN COALESCE(rows.image_count, 0) > 0 THEN 'image' ELSE 'token' END
                ELSE rows.billing_mode
            END AS billing_mode,
            COALESCE(NULLIF(TRIM(rows.inbound_endpoint), ''), 'unknown') AS inbound_endpoint
        FROM user_usage_analytics_delta_rows rows
        WHERE rows.user_id > 0
    ) changed
    WHERE target.user_id = changed.user_id
        AND target.bucket_start = changed.bucket_start
        AND target.api_key_id = changed.api_key_id
        AND target.group_id = changed.group_id
        AND target.requested_model = changed.requested_model
        AND target.request_type = changed.request_type
        AND target.stream = changed.stream
        AND target.openai_ws_mode = changed.openai_ws_mode
        AND target.billing_type = changed.billing_type
        AND target.billing_mode = changed.billing_mode
        AND target.inbound_endpoint = changed.inbound_endpoint
        AND target.requests <= 0;

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_usage_analytics_hourly_insert ON usage_logs;
CREATE TRIGGER trg_user_usage_analytics_hourly_insert
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS user_usage_analytics_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_usage_analytics_hourly_delta('1');

DROP TRIGGER IF EXISTS trg_user_usage_analytics_hourly_delete ON usage_logs;
CREATE TRIGGER trg_user_usage_analytics_hourly_delete
AFTER DELETE ON usage_logs
REFERENCING OLD TABLE AS user_usage_analytics_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_usage_analytics_hourly_delta('-1');

-- PostgreSQL fires same-event triggers in name order. Removing the old
-- contribution before adding the new one keeps dimension-changing updates
-- exact while leaving the raw usage-log write path untouched.
DROP TRIGGER IF EXISTS trg_user_usage_analytics_hourly_update_1_old ON usage_logs;
CREATE TRIGGER trg_user_usage_analytics_hourly_update_1_old
AFTER UPDATE ON usage_logs
REFERENCING OLD TABLE AS user_usage_analytics_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_usage_analytics_hourly_delta('-1');

DROP TRIGGER IF EXISTS trg_user_usage_analytics_hourly_update_2_new ON usage_logs;
CREATE TRIGGER trg_user_usage_analytics_hourly_update_2_new
AFTER UPDATE ON usage_logs
REFERENCING NEW TABLE AS user_usage_analytics_delta_rows
FOR EACH STATEMENT
EXECUTE FUNCTION apply_user_usage_analytics_hourly_delta('1');
