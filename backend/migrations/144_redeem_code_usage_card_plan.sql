-- Link usage-card redeem codes to an existing usage card plan.
-- Expand-only: old versions ignore the nullable column and keep running.

ALTER TABLE redeem_codes
    ADD COLUMN IF NOT EXISTS usage_card_plan_id BIGINT NULL REFERENCES usage_card_plans(id);

CREATE INDEX IF NOT EXISTS idx_redeem_codes_usage_card_plan_id
    ON redeem_codes (usage_card_plan_id);
