CREATE TABLE IF NOT EXISTS image_studio_jobs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    request_payload JSONB NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    size TEXT NOT NULL DEFAULT '',
    output_format TEXT NOT NULL DEFAULT 'png',
    estimated_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    charged_amount_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    billing_priority TEXT NOT NULL DEFAULT '',
    hold_balance_amount_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    hold_usage_card_amount_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    hold_usage_card_id BIGINT NULL,
    original_path TEXT NOT NULL DEFAULT '',
    thumbnail_path TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    file_size_bytes BIGINT NOT NULL DEFAULT 0,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ NULL,
    heartbeat_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NULL,
    assets_deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_user_queued
    ON image_studio_jobs (user_id, status, queued_at, id);

CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_status_started
    ON image_studio_jobs (status, started_at, heartbeat_at, id);

CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_assets_expiry
    ON image_studio_jobs (expires_at, assets_deleted_at, status, id);
