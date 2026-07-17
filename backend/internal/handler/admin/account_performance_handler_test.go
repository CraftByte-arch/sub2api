package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseAccountPerformanceFilterAllowsGlobalTimezone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h&timezone=Asia%2FShanghai", nil)

	filter, ok := parseAccountPerformanceFilter(c)

	require.True(t, ok)
	require.False(t, filter.start.IsZero())
	require.False(t, filter.end.IsZero())
	require.Equal(t, http.StatusOK, w.Code)
}
