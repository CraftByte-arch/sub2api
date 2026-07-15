CREATE TABLE IF NOT EXISTS first_token_timeout_stats_hourly (
    bucket_start TIMESTAMPTZ NOT NULL,
    scope VARCHAR(16) NOT NULL,
    account_id BIGINT NOT NULL DEFAULT 0,
    protocol VARCHAR(32) NOT NULL,
    platform VARCHAR(32) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    timeout_seconds SMALLINT NOT NULL,
    outcome VARCHAR(32) NOT NULL,
    failure_kind VARCHAR(32) NOT NULL DEFAULT '',
    sample_count BIGINT NOT NULL,
    ttft_sample_count BIGINT NOT NULL,
    ttft_sum_ms BIGINT NOT NULL,
    ttft_max_ms INTEGER NOT NULL,
    ttft_affected_count BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (
        bucket_start,
        scope,
        account_id,
        protocol,
        platform,
        model,
        timeout_seconds,
        outcome,
        failure_kind
    ),
    CHECK (scope IN ('attempt', 'request')),
    CHECK (timeout_seconds BETWEEN 1 AND 300),
    CHECK (sample_count >= 0),
    CHECK (ttft_sample_count >= 0),
    CHECK (ttft_sum_ms >= 0),
    CHECK (ttft_max_ms >= 0),
    CHECK (ttft_affected_count >= 0),
    CHECK (ttft_sample_count <= sample_count),
    CHECK (
        (ttft_sample_count = 0 AND ttft_sum_ms = 0 AND ttft_max_ms = 0)
        OR (ttft_sample_count > 0 AND ttft_max_ms <= ttft_sum_ms)
    ),
    CHECK (ttft_affected_count <= sample_count),
    CHECK (
        (scope = 'request' AND account_id = 0 AND platform = '')
        OR (scope = 'attempt' AND account_id > 0 AND platform <> '')
    ),
    CHECK (
        (scope = 'attempt' AND outcome IN (
            'success',
            'ttft_timeout',
            'client_canceled',
            'other_failure'
        ))
        OR (scope = 'request' AND outcome IN (
            'success',
            'recovered_after_ttft',
            'ttft_exhausted',
            'client_canceled',
            'other_failure'
        ))
    ),
    CHECK (
        (scope = 'request' AND ttft_sample_count = 0 AND ttft_sum_ms = 0 AND ttft_max_ms = 0)
        OR (scope = 'attempt' AND ttft_affected_count = 0)
    ),
    CHECK (
        scope <> 'request'
        OR (outcome = 'success' AND ttft_affected_count = 0)
        OR (outcome IN ('recovered_after_ttft', 'ttft_exhausted') AND ttft_affected_count = sample_count)
        OR (outcome IN ('other_failure', 'client_canceled') AND ttft_affected_count BETWEEN 0 AND sample_count)
    ),
    CHECK (NOT (
        scope = 'attempt' AND outcome = 'ttft_timeout' AND ttft_sample_count <> 0
    )),
    CHECK (
        (outcome = 'other_failure' AND failure_kind IN (
            'rate_limit',
            'auth',
            'upstream_4xx',
            'upstream_5xx',
            'transport',
            'stream_idle_timeout',
            'protocol',
            'other'
        ))
        OR (outcome <> 'other_failure' AND failure_kind = '')
    )
);

CREATE INDEX IF NOT EXISTS idx_first_token_timeout_stats_scope_bucket
    ON first_token_timeout_stats_hourly (scope, bucket_start);

CREATE INDEX IF NOT EXISTS idx_first_token_timeout_stats_scope_account_bucket
    ON first_token_timeout_stats_hourly (scope, account_id, bucket_start);

-- Down migration: DROP TABLE IF EXISTS first_token_timeout_stats_hourly;
-- The application migration runner executes each file as forward SQL and does not parse
-- Goose down sections, so the rollback statement must remain documentation-only here.
