package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstTokenTimeoutLowerStreamRecordersSkipAttemptError(t *testing.T) {
	tests := []struct {
		name   string
		record func(*OpenAIGatewayService, *gin.Context)
	}{
		{
			name: "responses and chat shared recorder",
			record: func(s *OpenAIGatewayService, c *gin.Context) {
				s.recordOpenAIStreamUpstreamError(c, &Account{ID: 1, Platform: PlatformOpenAI}, false, "", "failover", nil, "transport failed")
			},
		},
		{
			name: "anthropic messages recorder",
			record: func(s *OpenAIGatewayService, c *gin.Context) {
				s.recordOpenAIMessagesStreamUpstreamError(c, &Account{ID: 1, Platform: PlatformOpenAI}, "", "stream_missing_terminal", "transport failed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(ErrFirstTokenTimeout)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)

			tt.record(&OpenAIGatewayService{}, c)

			_, hasEvents := c.Get(OpsUpstreamErrorsKey)
			require.False(t, hasEvents)
			_, hasStatus := c.Get(OpsUpstreamStatusCodeKey)
			require.False(t, hasStatus)
		})
	}
}

func TestFirstTokenTimeoutLowerStreamRecordersKeepOrdinaryTransportError(t *testing.T) {
	tests := []struct {
		name   string
		record func(*OpenAIGatewayService, *gin.Context)
	}{
		{
			name: "responses and chat shared recorder",
			record: func(s *OpenAIGatewayService, c *gin.Context) {
				s.recordOpenAIStreamUpstreamError(c, &Account{ID: 1, Platform: PlatformOpenAI}, false, "", "failover", nil, "transport failed")
			},
		},
		{
			name: "anthropic messages recorder",
			record: func(s *OpenAIGatewayService, c *gin.Context) {
				s.recordOpenAIMessagesStreamUpstreamError(c, &Account{ID: 1, Platform: PlatformOpenAI}, "", "stream_missing_terminal", "transport failed")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", nil)

			tt.record(&OpenAIGatewayService{}, c)

			raw, hasEvents := c.Get(OpsUpstreamErrorsKey)
			require.True(t, hasEvents)
			events, ok := raw.([]*OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.Len(t, events, 1)
			status, hasStatus := c.Get(OpsUpstreamStatusCodeKey)
			require.True(t, hasStatus)
			require.Equal(t, 502, status)
		})
	}
}
