package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageCardRepositoryListCardsActiveExcludesExpiredAndExhausted(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "user_id", "plan_id", "name", "starts_at", "expires_at", "total_limit_usd",
		"used_usd", "status", "source", "source_order_id", "source_redeem_code",
		"assigned_by", "notes", "created_at", "updated_at", "deleted_at",
		"email", "username",
	}).AddRow(
		int64(1), int64(10), sql.NullInt64{}, "50 card", now.Add(-time.Hour), now.Add(time.Hour), 50.0,
		20.0, service.UsageCardStatusActive, service.UsageCardSourcePayment, sql.NullInt64{}, sql.NullString{},
		sql.NullInt64{}, sql.NullString{}, now, now, sql.NullTime{},
		sql.NullString{String: "user@example.com", Valid: true}, sql.NullString{String: "alice", Valid: true},
	)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\)[\\s\\S]*c\\.status = \\$1 AND c\\.expires_at > \\$2[\\s\\S]*c\\.total_limit_usd NOT IN[\\s\\S]*c\\.used_usd NOT IN[\\s\\S]*c\\.total_limit_usd > 0[\\s\\S]*c\\.used_usd >= 0[\\s\\S]*c\\.used_usd < c\\.total_limit_usd").
		WithArgs(service.UsageCardStatusActive, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(21)))
	mock.ExpectQuery("SELECT c\\.id[\\s\\S]*c\\.status = \\$1 AND c\\.expires_at > \\$2[\\s\\S]*c\\.total_limit_usd NOT IN[\\s\\S]*c\\.used_usd NOT IN[\\s\\S]*c\\.total_limit_usd > 0[\\s\\S]*c\\.used_usd >= 0[\\s\\S]*c\\.used_usd < c\\.total_limit_usd[\\s\\S]*ORDER BY c\\.status DESC[\\s\\S]*LIMIT \\$3 OFFSET \\$4").
		WithArgs(service.UsageCardStatusActive, sqlmock.AnyArg(), 20, 20).
		WillReturnRows(rows)

	cards, result, err := repo.ListCardsPaginated(context.Background(), nil, service.UsageCardStatusActive, pagination.PaginationParams{
		Page:      2,
		PageSize:  20,
		SortBy:    "status",
		SortOrder: pagination.SortOrderDesc,
	})
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, int64(21), result.Total)
	require.Equal(t, 2, result.Page)
	require.Equal(t, 20, result.PageSize)
	require.Equal(t, 2, result.Pages)
	require.Equal(t, int64(1), cards[0].ID)
	require.Equal(t, service.UsageCardStatusActive, cards[0].Status)
	require.NotNil(t, cards[0].User)
	require.Equal(t, "user@example.com", cards[0].User.Email)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryGetPlanByIDScansProductName(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)

	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT id, name, description, product_name, price, amount_usd, validity_days, features").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "description", "product_name", "price", "amount_usd", "validity_days", "features",
			"for_sale", "sort_order", "created_at", "updated_at",
		}).AddRow(
			int64(7), "Balance Card", "desc", "Credit Booster", 19.9, 25.0, 30, "fast\nflex",
			true, 3, now, now,
		))

	plan, err := repo.GetPlanByID(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, "Credit Booster", plan.ProductName)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryDeductCardRequiresAvailableActiveCard(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)

	now := time.Now()
	mock.ExpectQuery("deleted_at IS NULL[\\s\\S]*status = 'active'[\\s\\S]*starts_at <= \\$4[\\s\\S]*expires_at > \\$4[\\s\\S]*total_limit_usd NOT IN[\\s\\S]*used_usd NOT IN[\\s\\S]*total_limit_usd > 0[\\s\\S]*used_usd >= 0[\\s\\S]*used_usd < total_limit_usd[\\s\\S]*used_usd \\+ \\$1 <= total_limit_usd").
		WithArgs(2.0, int64(9), int64(10), now).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "plan_id", "name", "starts_at", "expires_at", "total_limit_usd",
			"used_usd", "status", "source", "source_order_id", "source_redeem_code",
			"assigned_by", "notes", "created_at", "updated_at", "deleted_at",
		}))

	card, err := repo.DeductCard(context.Background(), 9, 10, 2.0, now)
	require.Nil(t, card)
	require.ErrorIs(t, err, service.ErrUsageCardUnavailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryDeductCardRejectsInvalidAmount(t *testing.T) {
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		t.Run("invalid", func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := NewUsageCardRepository(db)

			card, err := repo.DeductCard(context.Background(), 9, 10, amount, time.Now())

			require.Nil(t, card)
			require.ErrorIs(t, err, service.ErrUsageCardUnavailable)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageCardRepositoryListAvailableCardsUsesBillingAvailabilityCriteria(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "plan_id", "name", "starts_at", "expires_at", "total_limit_usd",
		"used_usd", "status", "source", "source_order_id", "source_redeem_code",
		"assigned_by", "notes", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		int64(1), int64(42), sql.NullInt64{}, "active card", now.Add(-time.Hour), now.Add(time.Hour), 10.0,
		2.0, service.UsageCardStatusActive, service.UsageCardSourcePayment, sql.NullInt64{}, sql.NullString{},
		sql.NullInt64{}, sql.NullString{}, now, now, sql.NullTime{},
	)

	mock.ExpectQuery("status = 'active'[\\s\\S]*starts_at <= \\$2[\\s\\S]*expires_at > \\$2[\\s\\S]*total_limit_usd NOT IN[\\s\\S]*used_usd NOT IN[\\s\\S]*total_limit_usd > 0[\\s\\S]*used_usd >= 0[\\s\\S]*used_usd < total_limit_usd").
		WithArgs(int64(42), now).
		WillReturnRows(rows)

	cards, err := repo.ListAvailableCards(context.Background(), 42, now)

	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, int64(1), cards[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryConvertCardToBalanceUsesRemainingAmount(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)
	now := time.Now()
	startsAt := now.Add(-time.Hour)
	expiresAt := now.Add(time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT user_id, total_limit_usd, used_usd, status, starts_at, expires_at[\\s\\S]*FOR UPDATE").
		WithArgs(int64(21)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "total_limit_usd", "used_usd", "status", "starts_at", "expires_at"}).
			AddRow(int64(42), 20.0, 7.0, service.UsageCardStatusActive, startsAt, expiresAt))
	mock.ExpectQuery("UPDATE users[\\s\\S]*total_recharged[\\s\\S]*RETURNING balance").
		WithArgs(13.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(113.0))
	mock.ExpectExec("INSERT INTO redeem_codes").
		WithArgs(sqlmock.AnyArg(), service.AdjustmentTypeAdminBalance, 13.0, service.StatusUsed, int64(42), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE user_usage_cards[\\s\\S]*status = \\$4").
		WithArgs(service.UsageCardStatusCancelled, sqlmock.AnyArg(), int64(21), service.UsageCardStatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	conversion, err := repo.ConvertCardToBalance(context.Background(), 21, 7, "manual conversion")

	require.NoError(t, err)
	require.Equal(t, int64(42), conversion.UserID)
	require.InDelta(t, 13.0, conversion.AmountUSD, 0.000001)
	require.InDelta(t, 113.0, conversion.NewBalanceUSD, 0.000001)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryConvertCardToBalanceRejectsInvalidBalance(t *testing.T) {
	for _, tt := range []struct {
		name  string
		total float64
		used  float64
	}{
		{name: "negative used amount", total: 3000, used: -100},
		{name: "used amount exceeds total", total: 3000, used: 3000.01},
		{name: "infinite total", total: math.Inf(1), used: 0},
		{name: "not a number used", total: 3000, used: math.NaN()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := NewUsageCardRepository(db)
			now := time.Now()

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT user_id, total_limit_usd, used_usd, status, starts_at, expires_at[\\s\\S]*FOR UPDATE").
				WithArgs(int64(23)).
				WillReturnRows(sqlmock.NewRows([]string{"user_id", "total_limit_usd", "used_usd", "status", "starts_at", "expires_at"}).
					AddRow(int64(42), tt.total, tt.used, service.UsageCardStatusActive, now.Add(-time.Hour), now.Add(time.Hour)))
			mock.ExpectRollback()

			conversion, err := repo.ConvertCardToBalance(context.Background(), 23, 7, "")

			require.Nil(t, conversion)
			require.ErrorIs(t, err, service.ErrUsageCardInvalidBalance)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageCardRepositoryConvertCardToBalanceRejectsSuspendedCard(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT user_id, total_limit_usd, used_usd, status, starts_at, expires_at[\\s\\S]*FOR UPDATE").
		WithArgs(int64(24)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "total_limit_usd", "used_usd", "status", "starts_at", "expires_at"}).
			AddRow(int64(42), 20.0, 7.0, service.UsageCardStatusSuspended, now.Add(-time.Hour), now.Add(time.Hour)))
	mock.ExpectRollback()

	conversion, err := repo.ConvertCardToBalance(context.Background(), 24, 7, "")

	require.Nil(t, conversion)
	require.ErrorIs(t, err, service.ErrUsageCardUnavailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryConvertCardToBalanceRollsBackWhenAuditWriteFails(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT user_id, total_limit_usd, used_usd, status, starts_at, expires_at[\\s\\S]*FOR UPDATE").
		WithArgs(int64(25)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "total_limit_usd", "used_usd", "status", "starts_at", "expires_at"}).
			AddRow(int64(42), 20.0, 7.0, service.UsageCardStatusActive, now.Add(-time.Hour), now.Add(time.Hour)))
	mock.ExpectQuery("UPDATE users[\\s\\S]*total_recharged[\\s\\S]*RETURNING balance").
		WithArgs(13.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(113.0))
	mock.ExpectExec("INSERT INTO redeem_codes").
		WithArgs(sqlmock.AnyArg(), service.AdjustmentTypeAdminBalance, 13.0, service.StatusUsed, int64(42), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("audit insert failed"))
	mock.ExpectRollback()

	conversion, err := repo.ConvertCardToBalance(context.Background(), 25, 7, "")

	require.Nil(t, conversion)
	require.EqualError(t, err, "audit insert failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryConvertCardToBalanceRejectsUnavailableCard(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)
	now := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT user_id, total_limit_usd, used_usd, status, starts_at, expires_at[\\s\\S]*FOR UPDATE").
		WithArgs(int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "total_limit_usd", "used_usd", "status", "starts_at", "expires_at"}).
			AddRow(int64(42), 20.0, 20.0, service.UsageCardStatusExhausted, now.Add(-time.Hour), now.Add(time.Hour)))
	mock.ExpectRollback()

	conversion, err := repo.ConvertCardToBalance(context.Background(), 22, 7, "")

	require.Nil(t, conversion)
	require.ErrorIs(t, err, service.ErrUsageCardUnavailable)
	require.NoError(t, mock.ExpectationsWereMet())
}
