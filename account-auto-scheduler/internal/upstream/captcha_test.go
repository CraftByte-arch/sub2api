package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const testCaptchaImage = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestSub2APILoginChallengePreservesNoCaptchaBehavior(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   any
	}{
		{name: "explicitly disabled", status: http.StatusOK, body: map[string]any{"code": 0, "data": map[string]any{"local_captcha_enabled": false}}},
		{name: "settings unavailable", status: http.StatusBadGateway, body: map[string]any{"code": "UPSTREAM_ERROR"}},
		{name: "unrecognized response", status: http.StatusOK, body: map[string]any{"unexpected": true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var captchaCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/settings/public":
					writeTestJSON(t, w, test.status, test.body)
				case "/api/v1/auth/captcha":
					captchaCalls.Add(1)
					t.Fatal("CAPTCHA endpoint must not be called")
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			challenge, err := (sub2APIAdapter{}).StartLoginChallenge(context.Background(), server.URL)
			if err != nil || challenge.Required || captchaCalls.Load() != 0 {
				t.Fatalf("challenge=%#v err=%v captcha_calls=%d", challenge, err, captchaCalls.Load())
			}
		})
	}
	if _, ok := any(newAPIAdapter{}).(LoginChallengeAdapter); ok {
		t.Fatal("NewAPI unexpectedly enabled local CAPTCHA preflight")
	}
}

func TestSub2APILoginChallengeReturnsValidatedImageAndCookies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("challenge discovery sent authorization: %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/v1/settings/public":
			http.SetCookie(w, &http.Cookie{Name: "settings_session", Value: "one", HttpOnly: true})
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"local_captcha_enabled": true}})
		case "/api/v1/auth/captcha":
			if !strings.Contains(r.Header.Get("Cookie"), "settings_session=one") {
				t.Fatalf("CAPTCHA request did not preserve settings cookie: %q", r.Header.Get("Cookie"))
			}
			http.SetCookie(w, &http.Cookie{Name: "captcha_session", Value: "two", HttpOnly: true})
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"captcha_id": "captcha-123", "image_data": testCaptchaImage,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	challenge, err := (sub2APIAdapter{}).StartLoginChallenge(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !challenge.Required || challenge.Provider != localCaptchaProviderName || challenge.CaptchaID != "captcha-123" || challenge.ImageData != testCaptchaImage {
		t.Fatalf("unexpected challenge: %#v", challenge)
	}
	if !strings.Contains(challenge.Cookie, "settings_session=one") || !strings.Contains(challenge.Cookie, "captcha_session=two") {
		t.Fatalf("challenge cookies were not retained: %q", challenge.Cookie)
	}
}

func TestSub2APILoginChallengeRejectsExternalVerification(t *testing.T) {
	var captchaCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/settings/public":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"turnstile_enabled": true, "local_captcha_enabled": true,
			}})
		case "/api/v1/auth/captcha":
			captchaCalls.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := (sub2APIAdapter{}).StartLoginChallenge(context.Background(), server.URL)
	failure := AsAdapterError(err)
	if failure.Code != "CAPTCHA_REQUIRED" || failure.Status != model.IdentityStatusCaptcha || captchaCalls.Load() != 0 {
		t.Fatalf("failure=%#v captcha_calls=%d", failure, captchaCalls.Load())
	}
}

func TestCaptchaImageValidationRejectsUnsafeData(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "unsupported media", value: "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="},
		{name: "invalid base64", value: "data:image/png;base64,%%%"},
		{name: "empty image", value: "data:image/png;base64,"},
		{name: "oversized image", value: "data:image/png;base64," + strings.Repeat("A", 4*maxCaptchaImageBytes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeCaptchaImageData(test.value); AsAdapterError(err).Code != "UPSTREAM_CAPTCHA_IMAGE_INVALID" {
				t.Fatalf("error=%#v", err)
			}
		})
	}
	if _, err := normalizeCaptchaID("bad\nvalue"); AsAdapterError(err).Code != "UPSTREAM_CAPTCHA_INVALID" {
		t.Fatalf("captcha id error=%#v", err)
	}
}

func TestSub2APIPasswordLoginSubmitsCaptchaAndChallengeCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			if !strings.Contains(r.Header.Get("Cookie"), "captcha_session=server") {
				t.Fatalf("login omitted challenge cookie: %q", r.Header.Get("Cookie"))
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["email"] != "operator@example.com" || body["password"] != "password" || body["captcha_id"] != "captcha-id" || body["captcha_code"] != "AB12" {
				t.Fatalf("unexpected login payload: %#v", body)
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "final", HttpOnly: true})
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "access", "user": map[string]any{"id": 7, "email": "operator@example.com"},
			}})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer access" {
				t.Fatalf("verification omitted access token: %#v", r.Header)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
		Mode:            model.UpstreamAuthPassword,
		Username:        "operator@example.com",
		Password:        "password",
		CaptchaID:       "captcha-id",
		CaptchaCode:     "ab12",
		ChallengeCookie: "captcha_session=server",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Material.Cookie, "captcha_session=server") || !strings.Contains(result.Material.Cookie, "session=final") {
		t.Fatalf("successful material did not retain login cookies: %#v", result.Material)
	}
}

func TestSub2APIPasswordLoginWithoutChallengeOmitsCaptchaFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, exists := body["captcha_id"]; exists {
				t.Fatalf("ordinary login unexpectedly sent captcha_id: %#v", body)
			}
			if _, exists := body["captcha_code"]; exists {
				t.Fatalf("ordinary login unexpectedly sent captcha_code: %#v", body)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "access", "user": map[string]any{"id": 7, "email": "operator@example.com"},
			}})
		case "/api/v1/auth/me":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
		Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "password",
	}); err != nil {
		t.Fatal(err)
	}
}
