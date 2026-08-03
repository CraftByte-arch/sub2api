package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type invitationExpertSettingsRepoStub struct {
	service.SettingRepository
	enabled bool
}

func (r *invitationExpertSettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyAffiliateEnabled {
		return "", service.ErrSettingNotFound
	}
	if r.enabled {
		return "true", nil
	}
	return "false", nil
}

type invitationExpertAffiliateRepoStub struct {
	service.AffiliateRepository
	invitees     []service.AffiliateInvitee
	total        int64
	listCalls    int
	lastInviter  int64
	lastPage     int
	lastPageSize int
}

func (r *invitationExpertAffiliateRepoStub) ListInviteesPage(_ context.Context, inviterID int64, page, pageSize int) ([]service.AffiliateInvitee, int64, error) {
	r.listCalls++
	r.lastInviter = inviterID
	r.lastPage = page
	r.lastPageSize = pageSize
	return r.invitees, r.total, nil
}

func newInvitationExpertInviteesHandler(repo *invitationExpertAffiliateRepoStub, enabled bool) *UserHandler {
	settingService := service.NewSettingService(&invitationExpertSettingsRepoStub{enabled: enabled}, &config.Config{})
	affiliateService := service.NewAffiliateService(repo, settingService, nil, nil)
	return NewUserHandler(nil, nil, nil, nil, affiliateService, nil)
}

func performInvitationExpertInviteesRequest(
	t *testing.T,
	h *UserHandler,
	target string,
	withSubject bool,
	role string,
) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if withSubject {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		}
		if role != "" {
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
		c.Next()
	})
	router.GET("/api/v1/user/aff/invitees", h.GetInvitationExpertInvitees)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestGetInvitationExpertInviteesRequiresInvitationExpertRole(t *testing.T) {
	repo := &invitationExpertAffiliateRepoStub{}
	handler := newInvitationExpertInviteesHandler(repo, true)

	tests := []struct {
		name        string
		withSubject bool
		role        string
		wantStatus  int
	}{
		{name: "missing authentication", wantStatus: http.StatusUnauthorized},
		{name: "regular user", withSubject: true, role: service.RoleUser, wantStatus: http.StatusForbidden},
		{name: "administrator", withSubject: true, role: service.RoleAdmin, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := performInvitationExpertInviteesRequest(t, handler, "/api/v1/user/aff/invitees", tt.withSubject, tt.role)
			require.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
	require.Zero(t, repo.listCalls)
}

func TestGetInvitationExpertInviteesReturnsCurrentUsersUnmaskedEmail(t *testing.T) {
	createdAt := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	repo := &invitationExpertAffiliateRepoStub{
		invitees: []service.AffiliateInvitee{{
			UserID:      7,
			Email:       "invitee.original@example.com",
			Username:    "invitee",
			CreatedAt:   &createdAt,
			TotalRebate: 12.5,
		}},
		total: 101,
	}
	handler := newInvitationExpertInviteesHandler(repo, true)

	recorder := performInvitationExpertInviteesRequest(
		t,
		handler,
		"/api/v1/user/aff/invitees?page=2&page_size=250",
		true,
		service.RoleInvitationExpert,
	)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, repo.listCalls)
	require.Equal(t, int64(42), repo.lastInviter)
	require.Equal(t, 2, repo.lastPage)
	require.Equal(t, service.AffiliateInviteesMaxPageSize, repo.lastPageSize)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Items    []service.AffiliateInvitee `json:"items"`
			Total    int64                      `json:"total"`
			Page     int                        `json:"page"`
			PageSize int                        `json:"page_size"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, int64(101), body.Data.Total)
	require.Equal(t, 2, body.Data.Page)
	require.Equal(t, service.AffiliateInviteesMaxPageSize, body.Data.PageSize)
	require.Len(t, body.Data.Items, 1)
	require.Equal(t, "invitee.original@example.com", body.Data.Items[0].Email)
}

func TestGetInvitationExpertInviteesReturnsNotFoundWhenAffiliateFeatureDisabled(t *testing.T) {
	repo := &invitationExpertAffiliateRepoStub{}
	handler := newInvitationExpertInviteesHandler(repo, false)

	recorder := performInvitationExpertInviteesRequest(
		t,
		handler,
		"/api/v1/user/aff/invitees",
		true,
		service.RoleInvitationExpert,
	)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Zero(t, repo.listCalls)
}
