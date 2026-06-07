-- Usage card auto billing priority is now fixed to usage-card-first.
-- Keep this setting value aligned for databases that already applied 142.

UPDATE settings
SET value = 'usage_card_first', updated_at = NOW()
WHERE key = 'usage_card_default_priority';
