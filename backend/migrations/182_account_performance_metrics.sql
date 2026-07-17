-- Account performance aggregates are independent from usage billing. Rows represent
-- a single account/platform/group/model/protocol/outcome bucket in UTC.

CREATE TABLE IF NOT EXISTS account_performance_minute (
    bucket_start TIMESTAMPTZ NOT NULL,
    account_id BIGINT NOT NULL,
    platform VARCHAR(32) NOT NULL DEFAULT '',
    group_id BIGINT NOT NULL DEFAULT 0,
    model VARCHAR(255) NOT NULL DEFAULT '',
    protocol VARCHAR(32) NOT NULL DEFAULT '',
    outcome VARCHAR(32) NOT NULL,
    attempt_count BIGINT NOT NULL DEFAULT 0,
    success_count BIGINT NOT NULL DEFAULT 0,
    client_canceled_count BIGINT NOT NULL DEFAULT 0,
    ttft_timeout_count BIGINT NOT NULL DEFAULT 0,
    rate_limit_count BIGINT NOT NULL DEFAULT 0,
    auth_count BIGINT NOT NULL DEFAULT 0,
    upstream_4xx_count BIGINT NOT NULL DEFAULT 0,
    upstream_5xx_count BIGINT NOT NULL DEFAULT 0,
    transport_count BIGINT NOT NULL DEFAULT 0,
    protocol_count BIGINT NOT NULL DEFAULT 0,
    other_failure_count BIGINT NOT NULL DEFAULT 0,
    failover_count BIGINT NOT NULL DEFAULT 0,
    ttft_sample_count BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_sample_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_1000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_2500_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_5000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_10000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_30000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_gt_30000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_1000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_2500_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_5000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_10000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_30000_ms BIGINT NOT NULL DEFAULT 0,
    duration_gt_30000_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (bucket_start, account_id, platform, group_id, model, protocol, outcome),
    CHECK (outcome IN ('success', 'ttft_timeout', 'rate_limit', 'auth', 'upstream_4xx', 'upstream_5xx', 'transport', 'protocol', 'other_failure', 'client_canceled')),
    CHECK (attempt_count >= 0),
    CHECK (success_count >= 0),
    CHECK (client_canceled_count >= 0),
    CHECK (ttft_timeout_count >= 0),
    CHECK (rate_limit_count >= 0),
    CHECK (auth_count >= 0),
    CHECK (upstream_4xx_count >= 0),
    CHECK (upstream_5xx_count >= 0),
    CHECK (transport_count >= 0),
    CHECK (protocol_count >= 0),
    CHECK (other_failure_count >= 0),
    CHECK (failover_count >= 0),
    CHECK (ttft_sample_count >= 0),
    CHECK (ttft_sum_ms >= 0),
    CHECK (duration_sample_count >= 0),
    CHECK (duration_sum_ms >= 0),
    CHECK (success_count + client_canceled_count + ttft_timeout_count + rate_limit_count + auth_count + upstream_4xx_count + upstream_5xx_count + transport_count + protocol_count + other_failure_count = attempt_count),
    CHECK (
        (outcome = 'success' AND success_count = attempt_count)
        OR (outcome = 'client_canceled' AND client_canceled_count = attempt_count)
        OR (outcome = 'ttft_timeout' AND ttft_timeout_count = attempt_count)
        OR (outcome = 'rate_limit' AND rate_limit_count = attempt_count)
        OR (outcome = 'auth' AND auth_count = attempt_count)
        OR (outcome = 'upstream_4xx' AND upstream_4xx_count = attempt_count)
        OR (outcome = 'upstream_5xx' AND upstream_5xx_count = attempt_count)
        OR (outcome = 'transport' AND transport_count = attempt_count)
        OR (outcome = 'protocol' AND protocol_count = attempt_count)
        OR (outcome = 'other_failure' AND other_failure_count = attempt_count)
    ),
    CHECK (failover_count <= attempt_count),
    CHECK (ttft_sample_count <= success_count),
    CHECK (duration_sample_count <= success_count),
    CHECK ((ttft_sample_count = 0 AND ttft_sum_ms = 0) OR ttft_sample_count > 0),
    CHECK ((duration_sample_count = 0 AND duration_sum_ms = 0) OR duration_sample_count > 0),
    CHECK (ttft_le_1000_ms <= ttft_le_2500_ms AND ttft_le_2500_ms <= ttft_le_5000_ms AND ttft_le_5000_ms <= ttft_le_10000_ms AND ttft_le_10000_ms <= ttft_le_30000_ms),
    CHECK (duration_le_1000_ms <= duration_le_2500_ms AND duration_le_2500_ms <= duration_le_5000_ms AND duration_le_5000_ms <= duration_le_10000_ms AND duration_le_10000_ms <= duration_le_30000_ms),
    CHECK (ttft_le_30000_ms + ttft_gt_30000_ms = ttft_sample_count),
    CHECK (duration_le_30000_ms + duration_gt_30000_ms = duration_sample_count),
    CHECK (ttft_le_1000_ms >= 0 AND ttft_le_2500_ms >= 0 AND ttft_le_5000_ms >= 0 AND ttft_le_10000_ms >= 0 AND ttft_le_30000_ms >= 0 AND ttft_gt_30000_ms >= 0),
    CHECK (duration_le_1000_ms >= 0 AND duration_le_2500_ms >= 0 AND duration_le_5000_ms >= 0 AND duration_le_10000_ms >= 0 AND duration_le_30000_ms >= 0 AND duration_gt_30000_ms >= 0)
);

CREATE INDEX IF NOT EXISTS idx_account_performance_minute_bucket
    ON account_performance_minute (bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_minute_account_bucket
    ON account_performance_minute (account_id, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_minute_platform_bucket
    ON account_performance_minute (platform, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_minute_group_bucket
    ON account_performance_minute (group_id, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_minute_model_bucket
    ON account_performance_minute (model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_minute_filter
    ON account_performance_minute (bucket_start, platform, group_id, model, protocol);

CREATE TABLE IF NOT EXISTS account_performance_hourly (
    bucket_start TIMESTAMPTZ NOT NULL,
    account_id BIGINT NOT NULL,
    platform VARCHAR(32) NOT NULL DEFAULT '',
    group_id BIGINT NOT NULL DEFAULT 0,
    model VARCHAR(255) NOT NULL DEFAULT '',
    protocol VARCHAR(32) NOT NULL DEFAULT '',
    outcome VARCHAR(32) NOT NULL,
    attempt_count BIGINT NOT NULL DEFAULT 0,
    success_count BIGINT NOT NULL DEFAULT 0,
    client_canceled_count BIGINT NOT NULL DEFAULT 0,
    ttft_timeout_count BIGINT NOT NULL DEFAULT 0,
    rate_limit_count BIGINT NOT NULL DEFAULT 0,
    auth_count BIGINT NOT NULL DEFAULT 0,
    upstream_4xx_count BIGINT NOT NULL DEFAULT 0,
    upstream_5xx_count BIGINT NOT NULL DEFAULT 0,
    transport_count BIGINT NOT NULL DEFAULT 0,
    protocol_count BIGINT NOT NULL DEFAULT 0,
    other_failure_count BIGINT NOT NULL DEFAULT 0,
    failover_count BIGINT NOT NULL DEFAULT 0,
    ttft_sample_count BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_sample_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_1000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_2500_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_5000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_10000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_le_30000_ms BIGINT NOT NULL DEFAULT 0,
    ttft_gt_30000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_1000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_2500_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_5000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_10000_ms BIGINT NOT NULL DEFAULT 0,
    duration_le_30000_ms BIGINT NOT NULL DEFAULT 0,
    duration_gt_30000_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (bucket_start, account_id, platform, group_id, model, protocol, outcome),
    CHECK (outcome IN ('success', 'ttft_timeout', 'rate_limit', 'auth', 'upstream_4xx', 'upstream_5xx', 'transport', 'protocol', 'other_failure', 'client_canceled')),
    CHECK (attempt_count >= 0),
    CHECK (success_count >= 0),
    CHECK (client_canceled_count >= 0),
    CHECK (ttft_timeout_count >= 0),
    CHECK (rate_limit_count >= 0),
    CHECK (auth_count >= 0),
    CHECK (upstream_4xx_count >= 0),
    CHECK (upstream_5xx_count >= 0),
    CHECK (transport_count >= 0),
    CHECK (protocol_count >= 0),
    CHECK (other_failure_count >= 0),
    CHECK (failover_count >= 0),
    CHECK (ttft_sample_count >= 0),
    CHECK (ttft_sum_ms >= 0),
    CHECK (duration_sample_count >= 0),
    CHECK (duration_sum_ms >= 0),
    CHECK (success_count + client_canceled_count + ttft_timeout_count + rate_limit_count + auth_count + upstream_4xx_count + upstream_5xx_count + transport_count + protocol_count + other_failure_count = attempt_count),
    CHECK (
        (outcome = 'success' AND success_count = attempt_count)
        OR (outcome = 'client_canceled' AND client_canceled_count = attempt_count)
        OR (outcome = 'ttft_timeout' AND ttft_timeout_count = attempt_count)
        OR (outcome = 'rate_limit' AND rate_limit_count = attempt_count)
        OR (outcome = 'auth' AND auth_count = attempt_count)
        OR (outcome = 'upstream_4xx' AND upstream_4xx_count = attempt_count)
        OR (outcome = 'upstream_5xx' AND upstream_5xx_count = attempt_count)
        OR (outcome = 'transport' AND transport_count = attempt_count)
        OR (outcome = 'protocol' AND protocol_count = attempt_count)
        OR (outcome = 'other_failure' AND other_failure_count = attempt_count)
    ),
    CHECK (failover_count <= attempt_count),
    CHECK (ttft_sample_count <= success_count),
    CHECK (duration_sample_count <= success_count),
    CHECK ((ttft_sample_count = 0 AND ttft_sum_ms = 0) OR ttft_sample_count > 0),
    CHECK ((duration_sample_count = 0 AND duration_sum_ms = 0) OR duration_sample_count > 0),
    CHECK (ttft_le_1000_ms <= ttft_le_2500_ms AND ttft_le_2500_ms <= ttft_le_5000_ms AND ttft_le_5000_ms <= ttft_le_10000_ms AND ttft_le_10000_ms <= ttft_le_30000_ms),
    CHECK (duration_le_1000_ms <= duration_le_2500_ms AND duration_le_2500_ms <= duration_le_5000_ms AND duration_le_5000_ms <= duration_le_10000_ms AND duration_le_10000_ms <= duration_le_30000_ms),
    CHECK (ttft_le_30000_ms + ttft_gt_30000_ms = ttft_sample_count),
    CHECK (duration_le_30000_ms + duration_gt_30000_ms = duration_sample_count),
    CHECK (ttft_le_1000_ms >= 0 AND ttft_le_2500_ms >= 0 AND ttft_le_5000_ms >= 0 AND ttft_le_10000_ms >= 0 AND ttft_le_30000_ms >= 0 AND ttft_gt_30000_ms >= 0),
    CHECK (duration_le_1000_ms >= 0 AND duration_le_2500_ms >= 0 AND duration_le_5000_ms >= 0 AND duration_le_10000_ms >= 0 AND duration_le_30000_ms >= 0 AND duration_gt_30000_ms >= 0)
);

CREATE INDEX IF NOT EXISTS idx_account_performance_hourly_bucket
    ON account_performance_hourly (bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_hourly_account_bucket
    ON account_performance_hourly (account_id, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_hourly_platform_bucket
    ON account_performance_hourly (platform, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_hourly_group_bucket
    ON account_performance_hourly (group_id, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_hourly_model_bucket
    ON account_performance_hourly (model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_account_performance_hourly_filter
    ON account_performance_hourly (bucket_start, platform, group_id, model, protocol);

-- Down migration: DROP TABLE IF EXISTS account_performance_minute;
-- Down migration: DROP TABLE IF EXISTS account_performance_hourly;
-- The application migration runner executes each file as forward SQL and does not parse
-- Goose down sections, so the rollback statements must remain documentation-only here.
