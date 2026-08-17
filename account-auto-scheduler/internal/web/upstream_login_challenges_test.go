package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/upstream"
)

const webTestCaptchaImage = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

type fakeChallengeUpstreamConsole struct {
	*fakeUpstreamConsole
	challengeResult upstream.LoginChallengeResult
	challengeErr    error
	challengeInput  upstream.LoginChallengeInput
	challengeCalls  int
}

func (f *fakeChallengeUpstreamConsole) StartLoginChallenge(_ context.Context, _ string, input upstream.LoginChallengeInput) (upstream.LoginChallengeResult, error) {
	f.challengeCalls++
	f.challengeInput = input
	return f.challengeResult, f.challengeErr
}

func TestLoginChallengeStoreEnforcesScopeExpiryAndSingleUse(t *testing.T) {
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	store := newLoginChallengeStore(defaultLoginChallengeTTL)
	store.now = func() time.Time { return now }
	scope := loginChallengeScope{AdminID: 1, UpstreamID: "up_1", IdentityID: "identity", ManagementURL: "https://panel.example.com"}
	handle, err := store.Create(scope, upstream.LoginChallenge{
		Required: true, Provider: "local", CaptchaID: "upstream-captcha-id", ImageData: webTestCaptchaImage, Cookie: "captcha_session=secret",
	})
	if err != nil || handle.ID == "" || !handle.ExpiresAt.Equal(now.Add(defaultLoginChallengeTTL)) {
		t.Fatalf("handle=%#v err=%v", handle, err)
	}
	if _, err := store.Consume(handle.ID, loginChallengeScope{AdminID: 2, UpstreamID: "up_1", IdentityID: "identity", ManagementURL: "https://panel.example.com"}); !errors.Is(err, errLoginChallengeUnavailable) {
		t.Fatalf("wrong administrator error=%v", err)
	}
	attempt, err := store.Consume(handle.ID, scope)
	if err != nil || attempt.CaptchaID != "upstream-captcha-id" || attempt.Cookie != "captcha_session=secret" {
		t.Fatalf("attempt=%#v err=%v", attempt, err)
	}
	if _, err := store.Consume(handle.ID, scope); !errors.Is(err, errLoginChallengeUnavailable) {
		t.Fatalf("reused challenge error=%v", err)
	}

	expired, err := store.Create(scope, upstream.LoginChallenge{Required: true, Provider: "local", CaptchaID: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(defaultLoginChallengeTTL)
	if _, err := store.Consume(expired.ID, scope); !errors.Is(err, errLoginChallengeUnavailable) {
		t.Fatalf("expired challenge error=%v", err)
	}
}

func TestUpstreamLoginChallengeAPIRequiresAdminAndSkipsUnsupportedConsole(t *testing.T) {
	base := &fakeUpstreamConsole{}
	challengeConsole := &fakeChallengeUpstreamConsole{fakeUpstreamConsole: base}
	server := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: challengeConsole}, nil)

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/login-challenges", strings.NewReader(`{"auth_mode":"password","site_url":"https://panel.example.com"}`))
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || challengeConsole.challengeCalls != 0 {
		t.Fatalf("unauthorized status=%d calls=%d", unauthorizedResponse.Code, challengeConsole.challengeCalls)
	}

	tokenMode := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/login-challenges", strings.NewReader(`{"auth_mode":"token","site_url":"https://panel.example.com"}`))
	tokenMode.Header.Set("Authorization", "Bearer valid")
	tokenResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(tokenResponse, tokenMode)
	if tokenResponse.Code != http.StatusOK || challengeConsole.challengeCalls != 0 || !strings.Contains(tokenResponse.Body.String(), `"required":false`) {
		t.Fatalf("token preflight status=%d calls=%d body=%s", tokenResponse.Code, challengeConsole.challengeCalls, tokenResponse.Body.String())
	}

	plainServer := NewServer(nil, &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, Options{Upstreams: base}, nil)
	plain := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/login-challenges", strings.NewReader(`{"auth_mode":"password","site_url":"https://panel.example.com"}`))
	plain.Header.Set("Authorization", "Bearer valid")
	plainResponse := httptest.NewRecorder()
	plainServer.Handler().ServeHTTP(plainResponse, plain)
	if plainResponse.Code != http.StatusOK || !strings.Contains(plainResponse.Body.String(), `"required":false`) {
		t.Fatalf("unsupported console response=%d body=%s", plainResponse.Code, plainResponse.Body.String())
	}
}

func TestUpstreamLoginChallengeAPIHidesMaterialAndConsumesMatchingScope(t *testing.T) {
	base := &fakeUpstreamConsole{}
	challengeConsole := &fakeChallengeUpstreamConsole{
		fakeUpstreamConsole: base,
		challengeResult: upstream.LoginChallengeResult{
			ManagementURL: "https://panel.example.com",
			Challenge: upstream.LoginChallenge{
				Required: true, Provider: "local", CaptchaID: "raw-upstream-captcha-id", ImageData: webTestCaptchaImage, Cookie: "captcha_session=secret",
			},
		},
	}
	authorizer := &fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}
	server := NewServer(nil, authorizer, Options{Upstreams: challengeConsole}, nil)

	start := httptest.NewRequest(http.MethodPost, "/api/upstreams/up_1/login-challenges", strings.NewReader(`{"identity_id":"identity","auth_mode":"password","site_url":"https://panel.example.com"}`))
	start.Header.Set("Authorization", "Bearer admin-one")
	startResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(startResponse, start)
	if startResponse.Code != http.StatusOK || challengeConsole.challengeCalls != 1 || !challengeConsole.challengeInput.ManagementURLSet || challengeConsole.challengeInput.ManagementURL != "https://panel.example.com" {
		t.Fatalf("start status=%d calls=%d input=%#v body=%s", startResponse.Code, challengeConsole.challengeCalls, challengeConsole.challengeInput, startResponse.Body.String())
	}
	if strings.Contains(startResponse.Body.String(), "raw-upstream-captcha-id") || strings.Contains(startResponse.Body.String(), "captcha_session=secret") {
		t.Fatalf("challenge response leaked server-side material: %s", startResponse.Body.String())
	}
	var challenge upstreamLoginChallengeResponse
	if err := json.Unmarshal(startResponse.Body.Bytes(), &challenge); err != nil || !challenge.Required || challenge.ID == "" || challenge.ImageData != webTestCaptchaImage || challenge.ExpiresAt == nil {
		t.Fatalf("challenge=%#v err=%v", challenge, err)
	}

	authorizer.user = core.AdminUser{ID: 2, Role: "admin"}
	wrongAdmin := httptest.NewRequest(http.MethodPut, "/api/upstreams/up_1/identities/identity", strings.NewReader(`{"site_url":"https://panel.example.com","auth_mode":"password","username":"user","password":"secret","login_challenge_id":"`+challenge.ID+`","captcha_code":"AB12"}`))
	wrongAdmin.Header.Set("Authorization", "Bearer admin-two")
	wrongResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(wrongResponse, wrongAdmin)
	if wrongResponse.Code != http.StatusConflict || base.connectCalls != 0 {
		t.Fatalf("wrong administrator status=%d connect_calls=%d body=%s", wrongResponse.Code, base.connectCalls, wrongResponse.Body.String())
	}

	authorizer.user = core.AdminUser{ID: 1, Role: "admin"}
	connect := httptest.NewRequest(http.MethodPut, "/api/upstreams/up_1/identities/identity", strings.NewReader(`{"site_url":"https://panel.example.com","auth_mode":"password","username":"user","password":"secret","login_challenge_id":"`+challenge.ID+`","captcha_code":"ab12"}`))
	connect.Header.Set("Authorization", "Bearer admin-one-fresh")
	connectResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(connectResponse, connect)
	if connectResponse.Code != http.StatusOK || base.connectCalls != 1 {
		t.Fatalf("connect status=%d calls=%d body=%s", connectResponse.Code, base.connectCalls, connectResponse.Body.String())
	}
	if base.connectInput.Login.CaptchaID != "raw-upstream-captcha-id" || base.connectInput.Login.CaptchaCode != "ab12" || base.connectInput.Login.ChallengeCookie != "captcha_session=secret" {
		t.Fatalf("challenge material was not forwarded internally: %#v", base.connectInput.Login)
	}

	reuse := httptest.NewRequest(http.MethodPut, "/api/upstreams/up_1/identities/identity", strings.NewReader(`{"site_url":"https://panel.example.com","auth_mode":"password","username":"user","password":"secret","login_challenge_id":"`+challenge.ID+`","captcha_code":"AB12"}`))
	reuse.Header.Set("Authorization", "Bearer admin-one-reuse")
	reuseResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(reuseResponse, reuse)
	if reuseResponse.Code != http.StatusConflict || base.connectCalls != 1 {
		t.Fatalf("reuse status=%d calls=%d body=%s", reuseResponse.Code, base.connectCalls, reuseResponse.Body.String())
	}
}
