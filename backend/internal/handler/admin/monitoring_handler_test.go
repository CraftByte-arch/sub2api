package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMonitoringGetOverviewUnavailableWithoutDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestMonitoringGetOverviewRejectsInvalidRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=15m", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMonitoringGetOverviewRejectsUnknownQueryKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?range=24h&bogus=1", nil)

	handler := &MonitoringHandler{}
	handler.GetOverview(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
