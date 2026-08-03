package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageCardListRepositoryStub struct {
	service.UsageCardRepository
	userID       *int64
	status       string
	params       pagination.PaginationParams
	legacyCalled bool
}

func (s *usageCardListRepositoryStub) ListCards(_ context.Context, userID *int64, status string) ([]service.UserUsageCard, error) {
	s.userID = userID
	s.status = status
	s.legacyCalled = true
	return []service.UserUsageCard{}, nil
}

func (s *usageCardListRepositoryStub) ListCardsPaginated(
	_ context.Context,
	userID *int64,
	status string,
	params pagination.PaginationParams,
) ([]service.UserUsageCard, *pagination.PaginationResult, error) {
	s.userID = userID
	s.status = status
	s.params = params
	return []service.UserUsageCard{}, &pagination.PaginationResult{
		Total:    42,
		Page:     params.Page,
		PageSize: params.PageSize,
		Pages:    2,
	}, nil
}

func TestUsageCardHandlerListCardsReturnsPaginatedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &usageCardListRepositoryStub{}
	handler := NewUsageCardHandler(service.NewUsageCardService(repo, nil))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/usage-cards?page=2&page_size=25&status=expired&user_id=9&sort_by=status&sort_order=desc",
		nil,
	)

	handler.ListCards(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, repo.userID)
	require.Equal(t, int64(9), *repo.userID)
	require.Equal(t, service.UsageCardStatusExpired, repo.status)
	require.Equal(t, 2, repo.params.Page)
	require.Equal(t, 25, repo.params.PageSize)
	require.Equal(t, "status", repo.params.SortBy)
	require.Equal(t, pagination.SortOrderDesc, repo.params.SortOrder)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Items    []json.RawMessage `json:"items"`
			Total    int64             `json:"total"`
			Page     int               `json:"page"`
			PageSize int               `json:"page_size"`
			Pages    int               `json:"pages"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Zero(t, body.Code)
	require.Empty(t, body.Data.Items)
	require.Equal(t, int64(42), body.Data.Total)
	require.Equal(t, 2, body.Data.Page)
	require.Equal(t, 25, body.Data.PageSize)
	require.Equal(t, 2, body.Data.Pages)
}

func TestUsageCardHandlerListCardsKeepsLegacyArrayWithoutPaginationParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &usageCardListRepositoryStub{}
	handler := NewUsageCardHandler(service.NewUsageCardService(repo, nil))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage-cards?status=active", nil)

	handler.ListCards(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, repo.legacyCalled)
	require.Equal(t, service.UsageCardStatusActive, repo.status)

	var body struct {
		Code int               `json:"code"`
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Zero(t, body.Code)
	require.NotNil(t, body.Data)
	require.Empty(t, body.Data)
}
