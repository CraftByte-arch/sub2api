package repository

import (
	"context"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDeductFirstAvailableUsageCardRequiresAvailableActiveCard(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	mock.ExpectQuery("deleted_at IS NULL[\\s\\S]*status = 'active'[\\s\\S]*starts_at <= NOW\\(\\)[\\s\\S]*expires_at > NOW\\(\\)[\\s\\S]*total_limit_usd NOT IN[\\s\\S]*used_usd NOT IN[\\s\\S]*total_limit_usd > 0[\\s\\S]*used_usd >= 0[\\s\\S]*used_usd < total_limit_usd[\\s\\S]*used_usd \\+ \\$2 <= total_limit_usd").
		WithArgs(int64(10), 2.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	cardID, err := deductFirstAvailableUsageCard(context.Background(), tx, 10, 2.0)
	require.Nil(t, cardID)
	require.ErrorIs(t, err, service.ErrUsageCardUnavailable)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeductFirstAvailableUsageCardRejectsInvalidAmount(t *testing.T) {
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		t.Run("invalid", func(t *testing.T) {
			db, mock := newSQLMock(t)

			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			mock.ExpectRollback()

			cardID, err := deductFirstAvailableUsageCard(context.Background(), tx, 10, amount)

			require.Nil(t, cardID)
			require.ErrorIs(t, err, service.ErrUsageCardUnavailable)
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
