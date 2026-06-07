-- Additive group-level guard for discounted standard groups that must never
-- spend usage-card credit. Default false preserves existing behavior.

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS usage_card_disabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_groups_usage_card_disabled ON groups(usage_card_disabled);
