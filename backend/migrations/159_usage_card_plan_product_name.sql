-- Add an optional payment-provider product title for usage card plans.
-- The user-facing plan/card name remains usage_card_plans.name.

ALTER TABLE usage_card_plans
    ADD COLUMN IF NOT EXISTS product_name VARCHAR(100) NOT NULL DEFAULT '';
