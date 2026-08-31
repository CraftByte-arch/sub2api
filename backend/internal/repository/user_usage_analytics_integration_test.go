//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/pkg/userusageanalytics"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUserUsageAnalyticsMatchesLegacyAcrossUserFilters(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	legacy := newUsageLogRepositoryWithSQL(client, tx)
	store := newUserUsageAnalyticsStore(tx)
	decorated := &usageAggregationRepository{usageLogRepository: legacy, analytics: store}

	user := mustCreateUser(t, client, &service.User{Email: "usage-analytics-" + uuid.NewString() + "@example.com"})
	groupA := mustCreateGroup(t, client, &service.Group{Name: "usage-analytics-a-" + uuid.NewString(), Platform: service.PlatformOpenAI})
	groupB := mustCreateGroup(t, client, &service.Group{Name: "usage-analytics-b-" + uuid.NewString(), Platform: service.PlatformAnthropic})
	groupAID, groupBID := groupA.ID, groupB.ID
	keyA := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-analytics-a-" + uuid.NewString(), Name: "a", GroupID: &groupAID})
	keyB := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-analytics-b-" + uuid.NewString(), Name: "b", GroupID: &groupBID})
	account := mustCreateAccount(t, client, &service.Account{Name: "usage-analytics-" + uuid.NewString()})

	startTime := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	endTime := startTime.Add(48 * time.Hour)
	duration100, duration400 := 100, 400
	endpointResponses := "/v1/responses"
	endpointChat := "/v1/chat/completions"
	blankEndpoint := "   "
	accountRate := 2.0
	accountStatsCost := 0.25
	imageMode := ""
	perRequestMode := string(service.BillingModePerRequest)
	videoMode := string(service.BillingModeVideo)

	rows := []*service.UsageLog{
		{
			UserID: user.ID, APIKeyID: keyA.ID, AccountID: account.ID, GroupID: &groupAID,
			RequestID: uuid.NewString(), Model: "legacy-model", RequestedModel: "",
			InputTokens: 10, OutputTokens: 20, CacheCreationTokens: 3, CacheReadTokens: 4,
			TotalCost: 0.5, ActualCost: 0.4, AccountRateMultiplier: &accountRate, AccountStatsCost: &accountStatsCost,
			BillingType: service.BillingTypeBalance, DurationMs: &duration100,
			CreatedAt: startTime.Add(10 * time.Minute),
		},
		{
			UserID: user.ID, APIKeyID: keyA.ID, AccountID: account.ID, GroupID: &groupAID,
			RequestID: uuid.NewString(), Model: "mapped-gpt", RequestedModel: "gpt-5.4",
			InputTokens: 30, OutputTokens: 40, CacheCreationTokens: 5, CacheReadTokens: 6,
			TotalCost: 1.0, ActualCost: 0.8, BillingType: service.BillingTypeSubscription,
			ImageCount: 2, BillingMode: &imageMode, InboundEndpoint: &endpointResponses,
			CreatedAt: startTime.Add(80 * time.Minute),
		},
		{
			UserID: user.ID, APIKeyID: keyB.ID, AccountID: account.ID, GroupID: &groupBID,
			RequestID: uuid.NewString(), Model: "mapped-gpt-2", RequestedModel: "gpt-5.4",
			InputTokens: 50, OutputTokens: 60, CacheCreationTokens: 7, CacheReadTokens: 8,
			TotalCost: 1.5, ActualCost: 1.2, BillingType: service.BillingTypeUsageCard,
			BillingMode: &perRequestMode, InboundEndpoint: &endpointChat, DurationMs: &duration400,
			CreatedAt: startTime.Add(26 * time.Hour),
		},
		{
			UserID: user.ID, APIKeyID: keyB.ID, AccountID: account.ID,
			RequestID: uuid.NewString(), Model: "grok-video", RequestedModel: "grok-video",
			InputTokens: 2, OutputTokens: 3, CacheCreationTokens: 0, CacheReadTokens: 1,
			TotalCost: 2.0, ActualCost: 1.6, BillingType: service.BillingTypeBalance,
			BillingMode: &videoMode, InboundEndpoint: &blankEndpoint,
			CreatedAt: startTime.Add(27 * time.Hour),
		},
	}

	for _, row := range rows {
		inserted, err := legacy.Create(ctx, row)
		require.NoError(t, err)
		require.True(t, inserted)
	}

	legacyRequestTypes := []struct {
		id           int64
		requestType  int16
		stream       bool
		openAIWSMode bool
	}{
		{id: rows[0].ID, requestType: int16(service.RequestTypeUnknown), stream: false, openAIWSMode: false},
		{id: rows[1].ID, requestType: int16(service.RequestTypeUnknown), stream: true, openAIWSMode: false},
		{id: rows[2].ID, requestType: int16(service.RequestTypeUnknown), stream: true, openAIWSMode: true},
		{id: rows[3].ID, requestType: int16(service.RequestTypeLive), stream: false, openAIWSMode: false},
	}
	for _, update := range legacyRequestTypes {
		_, err := tx.ExecContext(ctx, `
			UPDATE usage_logs
			SET request_type = $2, stream = $3, openai_ws_mode = $4
			WHERE id = $1
		`, update.id, update.requestType, update.stream, update.openAIWSMode)
		require.NoError(t, err)
	}

	require.NoError(t, store.rebuildSegment(ctx, tx, startTime, endTime))
	require.NoError(t, store.verifySegment(ctx, tx, startTime, endTime))
	_, err := tx.ExecContext(ctx, `
		UPDATE user_usage_analytics_hourly_state
		SET ready = TRUE, coverage_start = $1, cursor = $2, updated_at = NOW()
		WHERE id = 1
	`, startTime, endTime)
	require.NoError(t, err)

	requestSync := int16(service.RequestTypeSync)
	requestStream := int16(service.RequestTypeStream)
	requestWS := int16(service.RequestTypeWSV2)
	streamTrue := true
	billingSubscription := int8(service.BillingTypeSubscription)
	tests := []struct {
		name   string
		mutate func(*UsageLogFilters)
	}{
		{name: "all"},
		{name: "api key", mutate: func(f *UsageLogFilters) { f.APIKeyID = keyA.ID }},
		{name: "group", mutate: func(f *UsageLogFilters) { f.GroupID = groupB.ID }},
		{name: "requested model fallback", mutate: func(f *UsageLogFilters) { f.Model = "legacy-model" }},
		{name: "requested model", mutate: func(f *UsageLogFilters) { f.Model = "gpt-5.4" }},
		{name: "legacy sync request type", mutate: func(f *UsageLogFilters) { f.RequestType = &requestSync }},
		{name: "legacy stream request type", mutate: func(f *UsageLogFilters) { f.RequestType = &requestStream }},
		{name: "legacy ws request type", mutate: func(f *UsageLogFilters) { f.RequestType = &requestWS }},
		{name: "stream boolean", mutate: func(f *UsageLogFilters) { f.Stream = &streamTrue }},
		{name: "billing type", mutate: func(f *UsageLogFilters) { f.BillingType = &billingSubscription }},
		{name: "implicit token mode", mutate: func(f *UsageLogFilters) { f.BillingMode = string(service.BillingModeToken) }},
		{name: "implicit image mode", mutate: func(f *UsageLogFilters) { f.BillingMode = string(service.BillingModeImage) }},
		{name: "per request mode", mutate: func(f *UsageLogFilters) { f.BillingMode = string(service.BillingModePerRequest) }},
		{name: "video mode", mutate: func(f *UsageLogFilters) { f.BillingMode = string(service.BillingModeVideo) }},
	}

	markedCtx := userusageanalytics.Mark(ctx)
	params := pagination.PaginationParams{Page: 1, PageSize: 2, SortBy: "created_at", SortOrder: "desc"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := UsageLogFilters{
				UserID:            user.ID,
				ModelFilterSource: usagestats.ModelSourceRequested,
				StartTime:         &startTime,
				EndTime:           &endTime,
			}
			if tt.mutate != nil {
				tt.mutate(&filters)
			}

			legacyLogs, legacyPage, err := legacy.ListWithFilters(ctx, params, filters)
			require.NoError(t, err)
			fastLogs, fastPage, err := decorated.ListWithFilters(markedCtx, params, filters)
			require.NoError(t, err)
			require.Equal(t, legacyPage, fastPage)
			require.Equal(t, usageLogIDsInOrder(legacyLogs), usageLogIDsInOrder(fastLogs))

			legacyStats, err := legacy.GetStatsWithFilters(ctx, filters)
			require.NoError(t, err)
			fastStats, err := decorated.GetStatsWithFilters(markedCtx, filters)
			require.NoError(t, err)
			requireUserUsageStatsEquivalent(t, legacyStats, fastStats)

			legacyTrend, err := legacy.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, "day", filters)
			require.NoError(t, err)
			fastTrend, err := decorated.GetUsageTrendWithUsageFilters(markedCtx, startTime, endTime, "day", filters)
			require.NoError(t, err)
			requireTrendStatsEquivalent(t, legacyTrend, fastTrend)

			legacyModels, err := legacy.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, filters, usagestats.ModelSourceRequested)
			require.NoError(t, err)
			fastModels, err := decorated.GetModelStatsWithUsageFiltersBySource(markedCtx, startTime, endTime, filters, usagestats.ModelSourceRequested)
			require.NoError(t, err)
			requireModelStatsEquivalent(t, legacyModels, fastModels)

			legacyGroups, err := legacy.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, filters)
			require.NoError(t, err)
			fastGroups, err := decorated.GetGroupStatsWithUsageFilters(markedCtx, startTime, endTime, filters)
			require.NoError(t, err)
			requireGroupStatsEquivalent(t, legacyGroups, fastGroups)
		})
	}
}

func TestUserUsageAnalyticsTriggerMaintainsInsertUpdateDelete(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "usage-analytics-trigger-" + uuid.NewString() + "@example.com"})
	group := mustCreateGroup(t, client, &service.Group{Name: "usage-analytics-trigger-" + uuid.NewString()})
	groupID := group.ID
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-analytics-trigger-" + uuid.NewString(), Name: "trigger"})
	account := mustCreateAccount(t, client, &service.Account{Name: "usage-analytics-trigger-" + uuid.NewString()})
	endpoint := "/v1/messages"
	duration := 120
	createdAt := time.Date(2026, 8, 29, 10, 15, 0, 0, time.UTC)
	usage := &service.UsageLog{
		UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, GroupID: &groupID,
		RequestID: uuid.NewString(), Model: "billing-model", RequestedModel: "requested-model",
		InputTokens: 10, OutputTokens: 20, CacheCreationTokens: 3, CacheReadTokens: 4,
		TotalCost: 0.5, ActualCost: 0.4, DurationMs: &duration, InboundEndpoint: &endpoint,
		CreatedAt: createdAt,
	}
	inserted, err := repo.Create(ctx, usage)
	require.NoError(t, err)
	require.True(t, inserted)

	assertUserUsageAnalyticsAggregate(t, tx, user.ID, "requested-model", "/v1/messages", 1, 10)

	_, err = tx.ExecContext(ctx, `
		UPDATE usage_logs
		SET requested_model = 'changed-model', inbound_endpoint = ' /v1/responses ', input_tokens = 25
		WHERE id = $1
	`, usage.ID)
	require.NoError(t, err)
	assertUserUsageAnalyticsAggregate(t, tx, user.ID, "requested-model", "/v1/messages", 0, 0)
	assertUserUsageAnalyticsAggregate(t, tx, user.ID, "changed-model", "/v1/responses", 1, 25)

	require.NoError(t, repo.Delete(ctx, usage.ID))
	assertUserUsageAnalyticsAggregate(t, tx, user.ID, "changed-model", "/v1/responses", 0, 0)
}

func assertUserUsageAnalyticsAggregate(t *testing.T, sqlq sqlExecutor, userID int64, model, endpoint string, requests, inputTokens int64) {
	t.Helper()
	var gotRequests, gotInputTokens int64
	err := scanSingleRow(context.Background(), sqlq, `
		SELECT COALESCE(SUM(requests), 0)::BIGINT, COALESCE(SUM(input_tokens), 0)::BIGINT
		FROM user_usage_analytics_hourly
		WHERE user_id = $1 AND requested_model = $2 AND inbound_endpoint = $3
	`, []any{userID, model, endpoint}, &gotRequests, &gotInputTokens)
	require.NoError(t, err)
	require.Equal(t, requests, gotRequests)
	require.Equal(t, inputTokens, gotInputTokens)
}

func usageLogIDsInOrder(logs []service.UsageLog) []int64 {
	ids := make([]int64, 0, len(logs))
	for _, row := range logs {
		ids = append(ids, row.ID)
	}
	return ids
}

func requireUserUsageStatsEquivalent(t *testing.T, expected, actual *usagestats.UsageStats) {
	t.Helper()
	require.Equal(t, expected.TotalRequests, actual.TotalRequests)
	require.Equal(t, expected.TotalInputTokens, actual.TotalInputTokens)
	require.Equal(t, expected.TotalOutputTokens, actual.TotalOutputTokens)
	require.Equal(t, expected.TotalCacheCreationTokens, actual.TotalCacheCreationTokens)
	require.Equal(t, expected.TotalCacheReadTokens, actual.TotalCacheReadTokens)
	require.Equal(t, expected.TotalCacheTokens, actual.TotalCacheTokens)
	require.Equal(t, expected.TotalTokens, actual.TotalTokens)
	require.InDelta(t, expected.TotalCost, actual.TotalCost, 1e-9)
	require.InDelta(t, expected.TotalActualCost, actual.TotalActualCost, 1e-9)
	require.InDelta(t, expected.AverageDurationMs, actual.AverageDurationMs, 1e-9)
	require.NotNil(t, expected.TotalAccountCost)
	require.NotNil(t, actual.TotalAccountCost)
	require.InDelta(t, *expected.TotalAccountCost, *actual.TotalAccountCost, 1e-9)
	require.Equal(t, expected.Endpoints, actual.Endpoints)
	require.Empty(t, actual.UpstreamEndpoints)
	require.Empty(t, actual.EndpointPaths)
}

func requireTrendStatsEquivalent(t *testing.T, expected, actual []usagestats.TrendDataPoint) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].Date, actual[i].Date)
		require.Equal(t, expected[i].Requests, actual[i].Requests)
		require.Equal(t, expected[i].InputTokens, actual[i].InputTokens)
		require.Equal(t, expected[i].OutputTokens, actual[i].OutputTokens)
		require.Equal(t, expected[i].CacheCreationTokens, actual[i].CacheCreationTokens)
		require.Equal(t, expected[i].CacheReadTokens, actual[i].CacheReadTokens)
		require.Equal(t, expected[i].TotalTokens, actual[i].TotalTokens)
		require.InDelta(t, expected[i].Cost, actual[i].Cost, 1e-9)
		require.InDelta(t, expected[i].ActualCost, actual[i].ActualCost, 1e-9)
	}
}

func requireModelStatsEquivalent(t *testing.T, expected, actual []usagestats.ModelStat) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].Model, actual[i].Model)
		require.Equal(t, expected[i].Requests, actual[i].Requests)
		require.Equal(t, expected[i].InputTokens, actual[i].InputTokens)
		require.Equal(t, expected[i].OutputTokens, actual[i].OutputTokens)
		require.Equal(t, expected[i].CacheCreationTokens, actual[i].CacheCreationTokens)
		require.Equal(t, expected[i].CacheReadTokens, actual[i].CacheReadTokens)
		require.Equal(t, expected[i].TotalTokens, actual[i].TotalTokens)
		require.InDelta(t, expected[i].Cost, actual[i].Cost, 1e-9)
		require.InDelta(t, expected[i].ActualCost, actual[i].ActualCost, 1e-9)
		require.InDelta(t, expected[i].AccountCost, actual[i].AccountCost, 1e-9)
	}
}

func requireGroupStatsEquivalent(t *testing.T, expected, actual []usagestats.GroupStat) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].GroupID, actual[i].GroupID)
		require.Equal(t, expected[i].GroupName, actual[i].GroupName)
		require.Equal(t, expected[i].Requests, actual[i].Requests)
		require.Equal(t, expected[i].TotalTokens, actual[i].TotalTokens)
		require.InDelta(t, expected[i].Cost, actual[i].Cost, 1e-9)
		require.InDelta(t, expected[i].ActualCost, actual[i].ActualCost, 1e-9)
		require.InDelta(t, expected[i].AccountCost, actual[i].AccountCost, 1e-9)
	}
}
