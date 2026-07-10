ALTER TABLE image_studio_jobs
    ADD COLUMN IF NOT EXISTS settlement_payload JSONB NOT NULL DEFAULT '{}'::jsonb;
