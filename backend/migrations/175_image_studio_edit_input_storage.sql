ALTER TABLE image_studio_jobs
    ADD COLUMN IF NOT EXISTS input_image_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS input_mask_path TEXT,
    ADD COLUMN IF NOT EXISTS input_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS input_deleted_at TIMESTAMPTZ;

UPDATE image_studio_jobs
SET request_payload = request_payload - 'images' - 'mask'
WHERE mode = 'edit'
  AND status IN ('succeeded', 'failed');

CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_input_cleanup
    ON image_studio_jobs (input_expires_at, input_deleted_at, status, id);
