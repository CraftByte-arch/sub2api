package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestParseAccountPerformanceFilterAcceptsSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h&search=prod", nil)

	filter, ok := parseAccountPerformanceFilter(c)

	require.True(t, ok)
	require.Equal(t, "prod", filter.search)
}

func TestParseAccountPerformanceFilterRejectsLongSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?search="+strings.Repeat("a", 256), nil)

	_, ok := parseAccountPerformanceFilter(c)

	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
