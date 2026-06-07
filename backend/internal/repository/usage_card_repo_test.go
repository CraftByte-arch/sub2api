package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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

	mock.ExpectQuery("c\\.status = 'active' AND c\\.expires_at > \\$1 AND c\\.used_usd < c\\.total_limit_usd").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	cards, err := repo.ListCards(context.Background(), nil, service.UsageCardStatusActive)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, int64(1), cards[0].ID)
	require.Equal(t, service.UsageCardStatusActive, cards[0].Status)
	require.NotNil(t, cards[0].User)
	require.Equal(t, "user@example.com", cards[0].User.Email)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCardRepositoryDeductCardRequiresAvailableActiveCard(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)

	now := time.Now()
	mock.ExpectQuery("deleted_at IS NULL[\\s\\S]*status = 'active'[\\s\\S]*starts_at <= \\$4[\\s\\S]*expires_at > \\$4[\\s\\S]*used_usd < total_limit_usd").
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
