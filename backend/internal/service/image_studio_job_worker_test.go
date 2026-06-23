package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsImageStudioRetryableError(t *testing.T) {
	t.Run("retryable upstream failover status", func(t *testing.T) {
		err := &UpstreamFailoverError{StatusCode: 429}
		require.True(t, isImageStudioRetryableError(err))
	})

	t.Run("retryable timeout", func(t *testing.T) {
		require.True(t, isImageStudioRetryableError(context.DeadlineExceeded))
	})

	t.Run("non-retryable validation", func(t *testing.T) {
		require.False(t, isImageStudioRetryableError(errors.New("model is required")))
	})
}

func TestImageStudioRetryDelay(t *testing.T) {
	require.Equal(t, 10*time.Second, imageStudioRetryDelay(1))
	require.Equal(t, 30*time.Second, imageStudioRetryDelay(2))
	require.Equal(t, 90*time.Second, imageStudioRetryDelay(3))
}
