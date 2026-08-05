-- Durable per-group usage-cost buckets for the administrator group summary.
-- Bucket timestamps are normalized to UTC by the repository implementation.
CREATE TABLE IF NOT EXISTS group_usage_hourly (
    group_id BIGINT NOT NULL,
    bucket_start TIMESTAMPTZ NOT NULL,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_group_usage_hourly_bucket_start
    ON group_usage_hourly (bucket_start);

-- A single durable row controls the one-time backfill and the read cutover.
CREATE TABLE IF NOT EXISTS group_usage_aggregation_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    ready BOOLEAN NOT NULL DEFAULT FALSE,
    cursor TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO group_usage_aggregation_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;
