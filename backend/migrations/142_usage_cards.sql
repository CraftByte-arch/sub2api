-- Usage cards are introduced as an additive, disabled-by-default wallet type.
-- The migration is intentionally expand-only so old application versions can
-- keep running during rolling deploys.

CREATE TABLE IF NOT EXISTS usage_card_plans (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price DECIMAL(20,2) NOT NULL,
    amount_usd DECIMAL(20,8) NOT NULL,
    validity_days INTEGER NOT NULL DEFAULT 30,
    features TEXT NOT NULL DEFAULT '',
    for_sale BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_usage_cards (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    plan_id BIGINT NULL REFERENCES usage_card_plans(id),
    name VARCHAR(100) NOT NULL DEFAULT '',
    starts_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    total_limit_usd DECIMAL(20,8) NOT NULL,
    used_usd DECIMAL(20,8) NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    source VARCHAR(20) NOT NULL DEFAULT 'admin',
    source_order_id BIGINT NULL,
    source_redeem_code VARCHAR(64) NULL,
    assigned_by BIGINT NULL REFERENCES users(id),
    notes TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS billing_priority VARCHAR(30) NOT NULL DEFAULT 'auto';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS usage_card_id BIGINT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_card_plans_for_sale ON usage_card_plans(for_sale);
CREATE INDEX IF NOT EXISTS idx_usage_card_plans_sort_order ON usage_card_plans(sort_order);

CREATE INDEX IF NOT EXISTS idx_user_usage_cards_user_id ON user_usage_cards(user_id);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_plan_id ON user_usage_cards(plan_id);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_status ON user_usage_cards(status);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_expires_at ON user_usage_cards(expires_at);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_user_status_expires ON user_usage_cards(user_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_source_order_id ON user_usage_cards(source_order_id);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_source_redeem_code ON user_usage_cards(source_redeem_code);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_assigned_by ON user_usage_cards(assigned_by);
CREATE INDEX IF NOT EXISTS idx_user_usage_cards_deleted_at ON user_usage_cards(deleted_at);

CREATE INDEX IF NOT EXISTS idx_api_keys_billing_priority ON api_keys(billing_priority);
CREATE INDEX IF NOT EXISTS idx_usage_logs_usage_card_id ON usage_logs(usage_card_id);

CREATE INDEX IF NOT EXISTS idx_user_usage_cards_available
    ON user_usage_cards(user_id, expires_at, created_at, id)
    WHERE deleted_at IS NULL AND status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_usage_cards_source_order_unique
    ON user_usage_cards(source_order_id)
    WHERE source_order_id IS NOT NULL AND deleted_at IS NULL;

INSERT INTO settings (key, value, updated_at)
VALUES
    ('usage_card_enabled', 'false', NOW()),
    ('usage_card_payment_enabled', 'false', NOW()),
    ('usage_card_redeem_enabled', 'false', NOW()),
    ('usage_card_billing_enabled', 'false', NOW()),
    ('usage_card_default_priority', 'balance_first', NOW()),
    ('legacy_subscription_purchase_enabled', 'true', NOW()),
    ('legacy_subscription_visible', 'true', NOW())
ON CONFLICT (key) DO NOTHING;
