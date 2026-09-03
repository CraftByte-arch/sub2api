package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/notify"
)

type fakeNotificationConsole struct {
	settings              notify.SettingsView
	saved                 notify.SettingsInput
	saveErr               error
	testErr               error
	clearCalls            int
	testCalls             int
	groupThresholds       map[int64]float64
	groupThresholdCalls   []thresholdCall
	accountThresholdCalls []thresholdCall
}

type thresholdCall struct {
	id        int64
	threshold *float64
}

func (f *fakeNotificationConsole) Settings() notify.SettingsView { return f.settings }

func (f *fakeNotificationConsole) SaveSettings(input notify.SettingsInput) (notify.SettingsView, error) {
	f.saved = input
	if f.saveErr != nil {
		return notify.SettingsView{}, f.saveErr
	}
	f.settings = notify.SettingsView{
		Enabled:             input.Enabled,
		Configured:          true,
		BarkEndpoint:        input.BarkEndpoint,
		BarkBasicAuthUser:   input.BarkBasicAuthUser,
		HasDeviceKey:        input.DeviceKey != "",
		HasEncryptionKey:    input.EncryptionKey != "",
		EncryptionAlgorithm: "AES-128-CBC",
	}
	return f.settings, nil
}

func (f *fakeNotificationConsole) ClearSettings() error {
	f.clearCalls++
	f.settings = notify.SettingsView{EncryptionAlgorithm: "AES-128-CBC"}
	return nil
}

func (f *fakeNotificationConsole) Test(context.Context) error {
	f.testCalls++
	return f.testErr
}

func (f *fakeNotificationConsole) GroupBalanceThresholds() map[int64]float64 {
	return f.groupThresholds
}

func (f *fakeNotificationConsole) GroupBalanceThreshold(groupID int64) (float64, bool) {
	value, ok := f.groupThresholds[groupID]
	return value, ok
}

func (f *fakeNotificationConsole) SetGroupBalanceThreshold(groupID int64, threshold *float64) error {
	f.groupThresholdCalls = append(f.groupThresholdCalls, thresholdCall{id: groupID, threshold: copyThreshold(threshold)})
	return nil
}

func (f *fakeNotificationConsole) SetAccountBalanceThreshold(accountID int64, threshold *float64) error {
	f.accountThresholdCalls = append(f.accountThresholdCalls, thresholdCall{id: accountID, threshold: copyThreshold(threshold)})
	return nil
}

func TestNotificationSettingsAPIRequiresAdminAndRedactsSecrets(t *testing.T) {
	notifications := &fakeNotificationConsole{}
	backend := &fakeConsoleCore{fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}}
	server := NewServer(nil, backend, Options{AuthCacheTTL: 0, Notifications: notifications}, nil)

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/notifications", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	body := `{"enabled":true,"bark_endpoint":"https://bark.example.com","bark_basic_auth_user":"bark-user","device_key":"device-secret","encryption_key":"1234567890123456","basic_auth_password":"password-secret"}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPut, "/api/notifications", body))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if notifications.saved.DeviceKey != "device-secret" || notifications.saved.BasicAuthPassword != "password-secret" {
		t.Fatalf("notification settings were not forwarded: %#v", notifications.saved)
	}
	for _, secret := range []string{"device-secret", "1234567890123456", "password-secret"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("response exposed %q: %s", secret, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), `"configured":true`) || !strings.Contains(response.Body.String(), `"encryption_algorithm":"AES-128-CBC"`) {
		t.Fatalf("unexpected redacted response: %s", response.Body.String())
	}
}

func TestNotificationTestFailureIsIsolated(t *testing.T) {
	notifications := &fakeNotificationConsole{testErr: errors.New("bark unavailable")}
	backend := &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}
	server := NewServer(nil, backend, Options{Notifications: notifications}, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/notifications/test", `{}`))
	if response.Code != http.StatusBadGateway || notifications.testCalls != 1 || !strings.Contains(response.Body.String(), "bark unavailable") {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, notifications.testCalls, response.Body.String())
	}

	session := httptest.NewRecorder()
	server.Handler().ServeHTTP(session, authenticatedRequest(http.MethodGet, "/api/session", ""))
	if session.Code != http.StatusOK {
		t.Fatalf("notification failure affected admin API: status=%d body=%s", session.Code, session.Body.String())
	}
}

func TestBalanceThresholdEndpoints(t *testing.T) {
	notifications := &fakeNotificationConsole{groupThresholds: map[int64]float64{10: 50}}
	backend := &fakeConsoleCore{
		fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}},
		groups:        []model.UpstreamGroup{{ID: 10, Name: "OpenAI", Status: "active"}},
		accounts:      []model.UpstreamAccount{{ID: 99, Name: "key", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true}},
	}
	server := newConsoleTestServer(t, backend, nil)
	server.notifications = notifications

	groupSet := httptest.NewRecorder()
	server.Handler().ServeHTTP(groupSet, authenticatedRequest(http.MethodPut, "/api/groups/10/balance-alert", `{"threshold":50}`))
	if groupSet.Code != http.StatusOK || len(notifications.groupThresholdCalls) != 1 || notifications.groupThresholdCalls[0].threshold == nil || *notifications.groupThresholdCalls[0].threshold != 50 {
		t.Fatalf("group set status=%d calls=%#v body=%s", groupSet.Code, notifications.groupThresholdCalls, groupSet.Body.String())
	}

	groupClear := httptest.NewRecorder()
	server.Handler().ServeHTTP(groupClear, authenticatedRequest(http.MethodDelete, "/api/groups/10/balance-alert", ""))
	if groupClear.Code != http.StatusNoContent || len(notifications.groupThresholdCalls) != 2 || notifications.groupThresholdCalls[1].threshold != nil {
		t.Fatalf("group clear status=%d calls=%#v", groupClear.Code, notifications.groupThresholdCalls)
	}

	accountSet := httptest.NewRecorder()
	server.Handler().ServeHTTP(accountSet, authenticatedRequest(http.MethodPut, "/api/configs/99/balance-alert", `{"threshold":10}`))
	if accountSet.Code != http.StatusOK || len(notifications.accountThresholdCalls) != 1 || notifications.accountThresholdCalls[0].id != 99 || *notifications.accountThresholdCalls[0].threshold != 10 {
		t.Fatalf("account set status=%d calls=%#v body=%s", accountSet.Code, notifications.accountThresholdCalls, accountSet.Body.String())
	}
	if configs := server.engine.List(); len(configs) != 1 || configs[0].AccountID != 99 || configs[0].Policy.Enabled || configs[0].NextCheckAt != nil {
		t.Fatalf("balance alert did not create a dormant passive record: %#v", configs)
	}
}

func copyThreshold(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
