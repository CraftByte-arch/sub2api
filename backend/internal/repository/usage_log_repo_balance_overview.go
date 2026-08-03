package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetBalanceOverview returns balances that are spendable at the time of the query.
func (r *usageLogRepository) GetBalanceOverview(ctx context.Context) (*service.BalanceOverview, error) {
	now := time.Now().UTC()
	overview := &service.BalanceOverview{GeneratedAt: now}
	var latestUsageCardExpiry sql.NullTime

	const query = `
		WITH user_balances AS (
			SELECT COALESCE(SUM(GREATEST(balance, 0)), 0) AS total_available_balance
			FROM users
			WHERE deleted_at IS NULL
		), available_usage_cards AS (
			SELECT
				COALESCE(SUM(GREATEST(c.total_limit_usd - c.used_usd, 0)), 0) AS total_usage_card_available_balance,
				MAX(c.expires_at) AS usage_card_latest_expires_at
			FROM user_usage_cards c
			JOIN users u ON u.id = c.user_id AND u.deleted_at IS NULL
			WHERE c.deleted_at IS NULL
				AND c.status = $1
				AND c.starts_at <= $2
				AND c.expires_at > $2
				AND c.total_limit_usd NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)
				AND c.used_usd NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)
				AND c.total_limit_usd > 0
				AND c.used_usd >= 0
				AND c.used_usd < c.total_limit_usd
		)
		SELECT
			user_balances.total_available_balance,
			available_usage_cards.total_usage_card_available_balance,
			available_usage_cards.usage_card_latest_expires_at
		FROM user_balances
		CROSS JOIN available_usage_cards
	`

	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{service.UsageCardStatusActive, now},
		&overview.TotalAvailableBalance,
		&overview.TotalUsageCardAvailableBalance,
		&latestUsageCardExpiry,
	); err != nil {
		return nil, err
	}

	if latestUsageCardExpiry.Valid {
		expiresAt := latestUsageCardExpiry.Time.UTC()
		overview.UsageCardLatestExpiresAt = &expiresAt
	}

	return overview, nil
}
