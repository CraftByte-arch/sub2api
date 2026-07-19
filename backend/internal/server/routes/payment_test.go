//go:build unit

package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentStatisticsRoutesAreStaticAndRegisteredBeforeOrderID(t *testing.T) {
	source, err := os.ReadFile("payment.go")
	require.NoError(t, err)
	text := string(source)
	dynamic := strings.Index(text, `orders.GET("/:id"`)
	summary := strings.Index(text, `orders.GET("/statistics"`)
	details := strings.Index(text, `orders.GET("/statistics/details"`)

	require.NotEqual(t, -1, dynamic)
	require.NotEqual(t, -1, summary)
	require.NotEqual(t, -1, details)
	require.Less(t, summary, dynamic)
	require.Less(t, details, dynamic)
}
