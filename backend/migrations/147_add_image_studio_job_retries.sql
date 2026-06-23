ALTER TABLE image_studio_jobs
    ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3,
    ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_next_attempt
    ON image_studio_jobs (status, next_attempt_at, queued_at, id);
