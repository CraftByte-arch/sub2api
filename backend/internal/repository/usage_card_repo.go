package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageCardRepository struct {
	db *sql.DB
}

func NewUsageCardRepository(db *sql.DB) service.UsageCardRepository {
	return &usageCardRepository{db: db}
}

func (r *usageCardRepository) ListPlans(ctx context.Context, includeHidden bool) ([]service.UsageCardPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	query := `
		SELECT id, name, description, price, amount_usd, validity_days, features,
			for_sale, sort_order, created_at, updated_at
		FROM usage_card_plans
	`
	args := []any{}
	if !includeHidden {
		query += " WHERE for_sale = TRUE"
	}
	query += " ORDER BY sort_order ASC, id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := make([]service.UsageCardPlan, 0)
	for rows.Next() {
		plan, err := scanUsageCardPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *plan)
	}
	return plans, rows.Err()
}

func (r *usageCardRepository) CreatePlan(ctx context.Context, plan service.UsageCardPlan) (*service.UsageCardPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO usage_card_plans (name, description, price, amount_usd, validity_days, features, for_sale, sort_order, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, description, price, amount_usd, validity_days, features,
			for_sale, sort_order, created_at, updated_at
	`, plan.Name, plan.Description, plan.Price, plan.AmountUSD, plan.ValidityDays, plan.Features, plan.ForSale, plan.SortOrder)
	return scanUsageCardPlan(row)
}

func (r *usageCardRepository) UpdatePlan(ctx context.Context, plan service.UsageCardPlan) (*service.UsageCardPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE usage_card_plans
		SET name = $1,
			description = $2,
			price = $3,
			amount_usd = $4,
			validity_days = $5,
			features = $6,
			for_sale = $7,
			sort_order = $8,
			updated_at = NOW()
		WHERE id = $9
		RETURNING id, name, description, price, amount_usd, validity_days, features,
			for_sale, sort_order, created_at, updated_at
	`, plan.Name, plan.Description, plan.Price, plan.AmountUSD, plan.ValidityDays, plan.Features, plan.ForSale, plan.SortOrder, plan.ID)
	out, err := scanUsageCardPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageCardPlanNotFound
	}
	return out, err
}

func (r *usageCardRepository) DeletePlan(ctx context.Context, id int64) error {
	if r == nil || r.db == nil {
		return errors.New("usage card repository db is nil")
	}
	res, err := r.db.ExecContext(ctx, `UPDATE usage_card_plans SET for_sale = FALSE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrUsageCardPlanNotFound
	}
	return nil
}

func (r *usageCardRepository) CreateCard(ctx context.Context, input service.CreateUsageCardInput) (*service.UserUsageCard, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO user_usage_cards (
			user_id, plan_id, name, starts_at, expires_at, total_limit_usd,
			used_usd, status, source, source_order_id, source_redeem_code,
			assigned_by, notes, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 0, $7, $8, $9, $10, $11, $12, NOW(), NOW())
		RETURNING id, user_id, plan_id, name, starts_at, expires_at, total_limit_usd,
			used_usd, status, source, source_order_id, source_redeem_code,
			assigned_by, notes, created_at, updated_at, deleted_at
	`,
		input.UserID,
		nullableInt64(input.PlanID),
		input.Name,
		input.StartsAt,
		input.ExpiresAt,
		input.TotalLimitUSD,
		service.UsageCardStatusActive,
		normalizeUsageCardSource(input.Source),
		nullableInt64(input.SourceOrderID),
		nullableString(input.SourceRedeemCode),
		nullableInt64(input.AssignedBy),
		nullableStringFromValue(input.Notes),
	)
	card, err := scanUserUsageCard(row)
	if err != nil {
		return nil, err
	}
	return card, nil
}

func (r *usageCardRepository) GetCardBySourceOrderID(ctx context.Context, orderID int64) (*service.UserUsageCard, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, plan_id, name, starts_at, expires_at, total_limit_usd,
			used_usd, status, source, source_order_id, source_redeem_code,
			assigned_by, notes, created_at, updated_at, deleted_at
		FROM user_usage_cards
		WHERE source_order_id = $1 AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT 1
	`, orderID)
	card, err := scanUserUsageCard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageCardNotFound
	}
	if err != nil {
		return nil, err
	}
	return card, nil
}

func (r *usageCardRepository) GetPlanByID(ctx context.Context, id int64) (*service.UsageCardPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, price, amount_usd, validity_days, features,
			for_sale, sort_order, created_at, updated_at
		FROM usage_card_plans
		WHERE id = $1
	`, id)
	plan, err := scanUsageCardPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageCardPlanNotFound
	}
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (r *usageCardRepository) ListAvailableCards(ctx context.Context, userID int64, now time.Time) ([]service.UserUsageCard, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, plan_id, name, starts_at, expires_at, total_limit_usd,
			used_usd, status, source, source_order_id, source_redeem_code,
			assigned_by, notes, created_at, updated_at, deleted_at
		FROM user_usage_cards
		WHERE user_id = $1
			AND deleted_at IS NULL
			AND status = 'active'
			AND starts_at <= $2
			AND expires_at > $2
			AND used_usd < total_limit_usd
		ORDER BY expires_at ASC, (total_limit_usd - used_usd) ASC, created_at ASC, id ASC
	`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := make([]service.UserUsageCard, 0)
	for rows.Next() {
		card, err := scanUserUsageCard(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *card)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cards, nil
}

func (r *usageCardRepository) ListUserCards(ctx context.Context, userID int64, includeDeleted bool) ([]service.UserUsageCard, error) {
	return r.listCards(ctx, &userID, "", includeDeleted)
}

func (r *usageCardRepository) ListCards(ctx context.Context, userID *int64, status string) ([]service.UserUsageCard, error) {
	return r.listCards(ctx, userID, status, false)
}

func (r *usageCardRepository) listCards(ctx context.Context, userID *int64, status string, includeDeleted bool) ([]service.UserUsageCard, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	query := `
		SELECT c.id, c.user_id, c.plan_id, c.name, c.starts_at, c.expires_at, c.total_limit_usd,
			c.used_usd, c.status, c.source, c.source_order_id, c.source_redeem_code,
			c.assigned_by, c.notes, c.created_at, c.updated_at, c.deleted_at,
			u.email, u.username
		FROM user_usage_cards c
		LEFT JOIN users u ON u.id = c.user_id
		WHERE 1 = 1
	`
	args := []any{}
	if userID != nil && *userID > 0 {
		args = append(args, *userID)
		query += fmt.Sprintf(" AND c.user_id = $%d", len(args))
	}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" AND c.status = $%d", len(args))
	}
	if !includeDeleted {
		query += " AND c.deleted_at IS NULL"
	}
	query += " ORDER BY c.expires_at ASC, c.created_at DESC, c.id DESC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := make([]service.UserUsageCard, 0)
	for rows.Next() {
		card, err := scanUserUsageCardWithUser(rows)
		if err != nil {
			return nil, err
		}
		cards = append(cards, *card)
	}
	return cards, rows.Err()
}

func (r *usageCardRepository) DeductCard(ctx context.Context, cardID, userID int64, amount float64, now time.Time) (*service.UserUsageCard, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("usage card repository db is nil")
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE user_usage_cards
		SET used_usd = used_usd + $1,
			status = CASE
				WHEN used_usd + $1 >= total_limit_usd THEN 'exhausted'
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2
			AND user_id = $3
			AND deleted_at IS NULL
			AND status = 'active'
			AND starts_at <= $4
			AND expires_at > $4
			AND used_usd < total_limit_usd
		RETURNING id, user_id, plan_id, name, starts_at, expires_at, total_limit_usd,
			used_usd, status, source, source_order_id, source_redeem_code,
			assigned_by, notes, created_at, updated_at, deleted_at
	`, amount, cardID, userID, now)
	card, err := scanUserUsageCard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUsageCardUnavailable
	}
	if err != nil {
		return nil, err
	}
	return card, nil
}

func (r *usageCardRepository) UpdateCardStatus(ctx context.Context, cardID int64, status string, reason string, operatorID int64) error {
	if r == nil || r.db == nil {
		return errors.New("usage card repository db is nil")
	}
	allowedCurrentStatusSQL := ""
	switch status {
	case service.UsageCardStatusSuspended:
		allowedCurrentStatusSQL = fmt.Sprintf(" AND status = '%s'", service.UsageCardStatusActive)
	case service.UsageCardStatusActive:
		allowedCurrentStatusSQL = fmt.Sprintf(" AND status = '%s'", service.UsageCardStatusSuspended)
	case service.UsageCardStatusCancelled:
		allowedCurrentStatusSQL = fmt.Sprintf(" AND status IN ('%s', '%s')", service.UsageCardStatusActive, service.UsageCardStatusSuspended)
	default:
		return service.ErrUsageCardUnavailable
	}
	note := fmt.Sprintf("admin status update: status=%s operator_id=%d reason=%s", status, operatorID, reason)
	query := `
		UPDATE user_usage_cards
		SET status = $1,
			notes = CASE
				WHEN notes IS NULL OR notes = '' THEN $2
				ELSE notes || E'\n' || $2
			END,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL
	` + allowedCurrentStatusSQL
	res, err := r.db.ExecContext(ctx, query, status, note, cardID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrUsageCardNotFound
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUsageCardPlan(row scanner) (*service.UsageCardPlan, error) {
	var plan service.UsageCardPlan
	if err := row.Scan(
		&plan.ID,
		&plan.Name,
		&plan.Description,
		&plan.Price,
		&plan.AmountUSD,
		&plan.ValidityDays,
		&plan.Features,
		&plan.ForSale,
		&plan.SortOrder,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &plan, nil
}

func scanUserUsageCard(row scanner) (*service.UserUsageCard, error) {
	var card service.UserUsageCard
	var planID sql.NullInt64
	var sourceOrderID sql.NullInt64
	var sourceRedeemCode sql.NullString
	var assignedBy sql.NullInt64
	var notes sql.NullString
	var deletedAt sql.NullTime
	if err := row.Scan(
		&card.ID,
		&card.UserID,
		&planID,
		&card.Name,
		&card.StartsAt,
		&card.ExpiresAt,
		&card.TotalLimitUSD,
		&card.UsedUSD,
		&card.Status,
		&card.Source,
		&sourceOrderID,
		&sourceRedeemCode,
		&assignedBy,
		&notes,
		&card.CreatedAt,
		&card.UpdatedAt,
		&deletedAt,
	); err != nil {
		return nil, err
	}
	card.PlanID = int64PtrFromNull(planID)
	card.SourceOrderID = int64PtrFromNull(sourceOrderID)
	card.SourceRedeemCode = stringPtrFromNull(sourceRedeemCode)
	card.AssignedBy = int64PtrFromNull(assignedBy)
	if notes.Valid {
		card.Notes = notes.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		card.DeletedAt = &t
	}
	return &card, nil
}

func scanUserUsageCardWithUser(row scanner) (*service.UserUsageCard, error) {
	var card service.UserUsageCard
	var planID sql.NullInt64
	var sourceOrderID sql.NullInt64
	var sourceRedeemCode sql.NullString
	var assignedBy sql.NullInt64
	var notes sql.NullString
	var deletedAt sql.NullTime
	var email sql.NullString
	var username sql.NullString
	if err := row.Scan(
		&card.ID,
		&card.UserID,
		&planID,
		&card.Name,
		&card.StartsAt,
		&card.ExpiresAt,
		&card.TotalLimitUSD,
		&card.UsedUSD,
		&card.Status,
		&card.Source,
		&sourceOrderID,
		&sourceRedeemCode,
		&assignedBy,
		&notes,
		&card.CreatedAt,
		&card.UpdatedAt,
		&deletedAt,
		&email,
		&username,
	); err != nil {
		return nil, err
	}
	card.PlanID = int64PtrFromNull(planID)
	card.SourceOrderID = int64PtrFromNull(sourceOrderID)
	card.SourceRedeemCode = stringPtrFromNull(sourceRedeemCode)
	card.AssignedBy = int64PtrFromNull(assignedBy)
	if notes.Valid {
		card.Notes = notes.String
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		card.DeletedAt = &t
	}
	if email.Valid || username.Valid {
		card.User = &service.UsageCardUser{
			ID:       card.UserID,
			Email:    email.String,
			Username: username.String,
		}
	}
	return &card, nil
}

func normalizeUsageCardSource(source string) string {
	switch source {
	case service.UsageCardSourcePayment, service.UsageCardSourceRedeem, service.UsageCardSourceAdmin, service.UsageCardSourceMigration:
		return source
	default:
		return service.UsageCardSourceAdmin
	}
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableStringFromValue(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func int64PtrFromNull(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func stringPtrFromNull(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	out := v.String
	return &out
}
