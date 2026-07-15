package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWebSearchStreamCommitsFirstTokenBeforeTextDelta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	attempt := NewFirstTokenAttempt(context.Background(), time.Hour)
	t.Cleanup(attempt.Close)
	commits := 0
	ctx := WithFirstTokenAttempt(attempt.Context(), attempt, func() error {
		commits++
		attempt.MarkFirstToken()
		return nil
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	searchResponse := &websearch.SearchResponse{Results: []websearch.SearchResult{{Title: "Result", URL: "https://example.com", Snippet: "summary"}}}

	_, err := writeWebSearchStreamResponse(ctx, c, "query", searchResponse, "claude", time.Now())

	require.NoError(t, err)
	require.Equal(t, 1, commits)
	require.Equal(t, FirstTokenCommitted, attempt.State())
	require.Contains(t, recorder.Body.String(), `"type":"text_delta"`)
}
