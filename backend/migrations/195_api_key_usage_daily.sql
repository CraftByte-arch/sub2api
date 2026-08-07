-- Durable per-API-key daily usage-cost buckets for batch usage summaries.
CREATE TABLE IF NOT EXISTS api_key_usage_daily (
    api_key_id BIGINT NOT NULL,
    bucket_date DATE NOT NULL,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (api_key_id, bucket_date)
);

-- A single durable row controls the resumable 30-day backfill and read cutover.
CREATE TABLE IF NOT EXISTS api_key_usage_daily_state (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    ready BOOLEAN NOT NULL DEFAULT FALSE,
    coverage_start DATE NULL,
    cursor DATE NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO api_key_usage_daily_state (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;
