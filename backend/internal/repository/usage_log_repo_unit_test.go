//go:build unit

package repository

import (
	"context"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetByRequestIDAndAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	usageCardID := int64(77)
	receipt := &service.UsageLog{
		ID:             99,
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "image-studio-job:39",
		Model:          "gpt-image-1",
		RequestedModel: "gpt-image-1",
		UsageCardID:    &usageCardID,
		ActualCost:     0.25,
		BillingType:    service.BillingTypeUsageCard,
		CreatedAt:      time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(receipt)
	values := make([]driver.Value, 0, len(prepared.args)+1)
	values = append(values, receipt.ID)
	for _, value := range prepared.args {
		values = append(values, value)
	}
	columns := strings.Split(usageLogSelectColumns, ", ")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+usageLogSelectColumns+" FROM usage_logs WHERE request_id = $1 AND api_key_id = $2")).
		WithArgs(receipt.RequestID, receipt.APIKeyID).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))

	repo := &usageLogRepository{sql: db}
	got, err := repo.GetByRequestIDAndAPIKey(context.Background(), receipt.RequestID, receipt.APIKeyID)

	require.NoError(t, err)
	require.Equal(t, receipt.ID, got.ID)
	require.Equal(t, receipt.ActualCost, got.ActualCost)
	require.Equal(t, service.BillingTypeUsageCard, got.BillingType)
	require.Equal(t, usageCardID, *got.UsageCardID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSafeDateFormat(t *testing.T) {
	tests := []struct {
		name        string
		granularity string
		expected    string
	}{
		// 合法值
		{"hour", "hour", "YYYY-MM-DD HH24:00"},
		{"day", "day", "YYYY-MM-DD"},
		{"week", "week", "IYYY-IW"},
		{"month", "month", "YYYY-MM"},

		// 非法值回退到默认
		{"空字符串", "", "YYYY-MM-DD"},
		{"未知粒度 year", "year", "YYYY-MM-DD"},
		{"未知粒度 minute", "minute", "YYYY-MM-DD"},

		// 恶意字符串
		{"SQL 注入尝试", "'; DROP TABLE users; --", "YYYY-MM-DD"},
		{"带引号", "day'", "YYYY-MM-DD"},
		{"带括号", "day)", "YYYY-MM-DD"},
		{"Unicode", "日", "YYYY-MM-DD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeDateFormat(tc.granularity)
			require.Equal(t, tc.expected, got, "safeDateFormat(%q)", tc.granularity)
		})
	}
}

func TestBuildUsageLogBatchInsertQuery_UsesConflictDoNothing(t *testing.T) {
	log := &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-batch-no-update",
		Model:        "gpt-5",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.2,
		ActualCost:   1.2,
		CreatedAt:    time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	query, _ := buildUsageLogBatchInsertQuery([]string{usageLogBatchKey(log.RequestID, log.APIKeyID)}, map[string]usageLogInsertPrepared{
		usageLogBatchKey(log.RequestID, log.APIKeyID): prepared,
	})

	require.Contains(t, query, "ON CONFLICT (request_id, api_key_id) DO NOTHING")
	require.NotContains(t, strings.ToUpper(query), "DO UPDATE")
}
