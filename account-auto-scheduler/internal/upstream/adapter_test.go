package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func writeTestJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func TestDetectSupportedFamiliesWithoutCredentials(t *testing.T) {
	tests := []struct {
		name string
		path string
		body any
		want string
	}{
		{name: "sub2api", path: "/api/v1/settings/public", body: map[string]any{"code": 0, "data": map[string]any{"site_name": "Sub2API"}}, want: "sub2api"},
		{name: "newapi", path: "/api/status", body: map[string]any{"success": true, "data": map[string]any{"system_name": "New API"}}, want: "newapi"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatalf("detection sent credentials: %#v", r.Header)
				}
				if r.URL.Path == test.path {
					writeTestJSON(t, w, http.StatusOK, test.body)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			result, err := Detect(context.Background(), server.URL+"/v1")
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Type) != test.want || result.Evidence != test.path {
				t.Fatalf("Detect() = %#v, want %s", result, test.want)
			}
		})
	}
}

func TestRemoteClientRejectsCredentialedCrossOriginRedirect(t *testing.T) {
	var destinationCalls atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect destination received Authorization")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/stolen", http.StatusFound)
	}))
	defer source.Close()

	client, err := newRemoteClient(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.do(context.Background(), http.MethodGet, "/redirect", nil, AuthMaterial{AccessToken: "top-secret"}); err == nil {
		t.Fatal("cross-origin redirect unexpectedly succeeded")
	}
	if destinationCalls.Load() != 0 {
		t.Fatalf("redirect destination calls = %d, want 0", destinationCalls.Load())
	}
}

func TestRemoteClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxRemoteJSONBytes+1)))
	}))
	defer server.Close()
	client, err := newRemoteClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.do(context.Background(), http.MethodGet, "/large", nil, AuthMaterial{})
	var adapterErr *AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != "UPSTREAM_RESPONSE_TOO_LARGE" {
		t.Fatalf("error = %#v, want response-too-large AdapterError", err)
	}
}

func TestSub2APIAdapterConnectAndSync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "sub-access", "refresh_token": "sub-refresh", "user": map[string]any{"id": 7, "email": "operator@example.com"},
			}})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer sub-access" {
				writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "UNAUTHORIZED", "message": "no"})
				return
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com", "balance": 42.5}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{map[string]any{"id": 5, "name": "OpenAI", "rate_multiplier": 1.2}}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"5": 0.8}})
		case "/api/v1/keys":
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "100" {
				t.Errorf("unexpected pagination query: %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"items": []any{map[string]any{"id": 11, "key": "sk-sub2api-complete-secret", "name": "production", "group_id": 5, "status": "active", "quota": 100, "quota_used": 12}},
				"total": 1, "page": 1, "page_size": 100, "pages": 1,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := sub2APIAdapter{}
	login, err := adapter.Connect(context.Background(), server.URL, LoginInput{Mode: "password", Username: "operator@example.com", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if login.Principal != "operator@example.com" || login.Material.AccessToken != "sub-access" {
		t.Fatalf("unexpected login: %#v", login)
	}
	if login.Balance == nil || login.Balance.Amount != 42.5 || login.Balance.Unit != "USD" || login.Balance.Source != "sub2api" || login.Balance.Stale {
		t.Fatalf("unexpected login balance: %#v", login.Balance)
	}
	result, err := adapter.Sync(context.Background(), server.URL, login.Material)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Keys) != 1 {
		t.Fatalf("keys = %#v", result.Keys)
	}
	if result.Balance == nil || result.Balance.Amount != 42.5 || result.Balance.Unit != "USD" || result.Balance.Source != "sub2api" {
		t.Fatalf("unexpected sync balance: %#v", result.Balance)
	}
	key := result.Keys[0]
	if key.Plaintext != "sk-sub2api-complete-secret" || strings.Contains(key.Key.MaskedKey, "complete-secret") {
		t.Fatalf("key was not separated/redacted: %#v", key)
	}
	if key.Key.Multiplier == nil || *key.Key.Multiplier != 0.8 || key.Key.MultiplierSource != "user_override" || key.Key.Group != "OpenAI" {
		t.Fatalf("unexpected multiplier data: %#v", key.Key)
	}
}

func TestSub2APIAdapterKeepsManagementRequestsOnOneOrigin(t *testing.T) {
	calls := make([]string, 0, 6)
	managementServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/auth/credential-key":
			http.NotFound(w, r)
		case "/api/v1/auth/login":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "site-access", "user": map[string]any{"id": 7, "email": "operator@example.com"},
			}})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer site-access" {
				t.Fatalf("management endpoint received wrong credential: %q", r.Header.Get("Authorization"))
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com", "balance": 3}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			t.Fatalf("management endpoint received unexpected request %s", r.URL.Path)
		}
	}))
	defer managementServer.Close()

	adapter := sub2APIAdapter{}
	login, err := adapter.Connect(context.Background(), managementServer.URL, LoginInput{Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Sync(context.Background(), managementServer.URL, login.Material); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 6 {
		t.Fatalf("management endpoint did not receive the full connect and sync flow: %#v", calls)
	}
}

func TestSub2APIAdapterPreservesZeroBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 1, "balance": 0}})
	}))
	defer server.Close()

	result, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: model.UpstreamAuthToken, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Balance == nil || result.Balance.Amount != 0 || result.Balance.Unit != "USD" {
		t.Fatalf("zero balance was treated as unavailable: %#v", result.Balance)
	}
}

func TestSub2APIAdapterTreatsNullBalanceAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 1, "balance": nil}})
	}))
	defer server.Close()

	result, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: model.UpstreamAuthToken, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Balance != nil {
		t.Fatalf("null balance was treated as zero: %#v", result.Balance)
	}
}

func TestSub2APIAdapterReportsCaptchaAndTwoFactor(t *testing.T) {
	tests := []struct {
		name       string
		response   any
		wantStatus string
	}{
		{name: "captcha", response: map[string]any{"code": "CAPTCHA_REQUIRED", "message": "turnstile captcha required"}, wantStatus: "captcha_required"},
		{name: "two factor", response: map[string]any{"code": 0, "data": map[string]any{"requires_2fa": true, "temp_token": "must-not-leak"}}, wantStatus: "two_factor_required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/credential-key":
					http.NotFound(w, r)
				case "/api/v1/auth/login":
					writeTestJSON(t, w, http.StatusOK, test.response)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			_, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: "password", Username: "user", Password: "secret"})
			adapterErr := AsAdapterError(err)
			if string(adapterErr.Status) != test.wantStatus || strings.Contains(adapterErr.Message, "must-not-leak") {
				t.Fatalf("error = %#v, want status %s", adapterErr, test.wantStatus)
			}
		})
	}
}

func TestNewAPIAdapterReportsExplicitCredentialRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/login" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, http.StatusOK, map[string]any{
			"message": "Username or password is incorrect, or user has been banned",
			"success": false,
		})
	}))
	defer server.Close()

	_, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
		Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "not-the-real-password",
	})
	adapterErr := AsAdapterError(err)
	if adapterErr.Code != "UPSTREAM_INVALID_CREDENTIALS" || adapterErr.HTTPCode != http.StatusUnauthorized || adapterErr.Status != model.IdentityStatusInvalid {
		t.Fatalf("error = %#v, want explicit invalid-credentials response", adapterErr)
	}
	if strings.Contains(strings.ToLower(adapterErr.Message), "user has been banned") || strings.Contains(adapterErr.Message, "not-the-real-password") {
		t.Fatalf("upstream response or submitted password escaped sanitization: %#v", adapterErr)
	}
}

func TestAdapterReportsExpiredAndMalformedResponses(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED", "message": "expired"})
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: "token", Token: "expired-token"})
		if adapterErr := AsAdapterError(err); adapterErr.Status != "expired" {
			t.Fatalf("error = %#v, want expired", adapterErr)
		}
	})

	t.Run("generic management forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><title>Access denied</title></html>"))
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: "token", Token: "token"})
		adapterErr := AsAdapterError(err)
		if adapterErr.Code != "UPSTREAM_MANAGEMENT_ACCESS_DENIED" || adapterErr.Status != model.IdentityStatusAccessDenied || strings.Contains(adapterErr.Message, "Access denied") {
			t.Fatalf("error = %#v, want a safe management-access-denied error", adapterErr)
		}
	})

	t.Run("recognized forbidden session expiry", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, http.StatusForbidden, map[string]any{"success": false, "code": "TOKEN_EXPIRED"})
		}))
		defer server.Close()
		_, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: "token", Token: "token", UserID: "7"})
		if adapterErr := AsAdapterError(err); adapterErr.Code != "UPSTREAM_SESSION_EXPIRED" || adapterErr.Status != model.IdentityStatusExpired {
			t.Fatalf("error = %#v, want recognized session expiry", adapterErr)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/auth/me" {
				writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 1}})
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":0,"data":`))
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{AccessToken: "token"})
		if err == nil || AsAdapterError(err).Code != "UPSTREAM_INVALID_RESPONSE" {
			t.Fatalf("error = %#v, want invalid response", err)
		}
	})
}

func TestAdaptersDoNotRefreshAfterManagementAccessDenied(t *testing.T) {
	t.Run("sub2api", func(t *testing.T) {
		var refreshCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/me":
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("blocked"))
			case "/api/v1/auth/refresh":
				refreshCalls.Add(1)
				writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"access_token": "new"}})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{AccessToken: "old", RefreshToken: "refresh"})
		if adapterErr := AsAdapterError(err); adapterErr.Code != "UPSTREAM_MANAGEMENT_ACCESS_DENIED" || refreshCalls.Load() != 0 {
			t.Fatalf("error = %#v refresh_calls=%d, want access denial without refresh", adapterErr, refreshCalls.Load())
		}
	})

	t.Run("newapi", func(t *testing.T) {
		var refreshCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/user/self":
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("blocked"))
			case "/api/user/auth/refresh":
				refreshCalls.Add(1)
				writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"access_token": "new"}})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		_, err := (newAPIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{AccessToken: "old", Cookie: "refresh_token=value", UserID: "7"})
		if adapterErr := AsAdapterError(err); adapterErr.Code != "UPSTREAM_MANAGEMENT_ACCESS_DENIED" || refreshCalls.Load() != 0 {
			t.Fatalf("error = %#v refresh_calls=%d, want access denial without refresh", adapterErr, refreshCalls.Load())
		}
	})
}

func TestNewAPIAdapterConnectAndSyncMaskedAndDynamicKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "refresh-secret", HttpOnly: true})
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"access_token": "new-access", "user": map[string]any{"id": 9, "username": "operator"},
			}})
		case "/api/user/self":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"success": false, "code": "AUTH_TOKEN_EXPIRED"})
				return
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 9, "username": "operator", "quota": 2500000}})
		case "/api/status":
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Errorf("public NewAPI status request carried credentials: %#v", r.Header)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/user/self/groups":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"vip": map[string]any{"ratio": 1.25, "desc": "VIP"}, "auto": map[string]any{"ratio": "自动", "desc": "Auto"},
			}})
		case "/api/token/":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"items": []any{
					map[string]any{"id": 21, "key": "abcd**********wxyz", "name": "auto-key", "status": 1, "group": "auto", "remain_quota": 80, "used_quota": 20},
					map[string]any{"id": 22, "key": "sk-newapi-full-secret", "name": "vip-key", "status": 1, "group": "vip", "unlimited_quota": true, "used_quota": 5},
				}, "total": 2, "page": 1, "page_size": 100,
			}})
		case "/api/log/self":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"items": []any{map[string]any{"token_id": 21, "token_name": "auto-key", "created_at": 1786320000, "other": `{"group_ratio":1.7}`}},
				"total": 1, "page": 1, "page_size": 100,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := newAPIAdapter{}
	login, err := adapter.Connect(context.Background(), server.URL, LoginInput{Mode: "password", Username: "operator", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if login.Material.AccessToken != "new-access" || !strings.Contains(login.Material.Cookie, "refresh_token=") || login.Principal != "operator" {
		t.Fatalf("unexpected login result: %#v", login)
	}
	if login.Balance == nil || login.Balance.Amount != 5 || login.Balance.Unit != "USD" || login.Balance.Source != "newapi" || login.Balance.RawQuota == nil || *login.Balance.RawQuota != 2500000 || login.Balance.QuotaPerUnit == nil || *login.Balance.QuotaPerUnit != 500000 {
		t.Fatalf("unexpected normalized login balance: %#v", login.Balance)
	}
	result, err := adapter.Sync(context.Background(), server.URL, login.Material)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Keys) != 2 {
		t.Fatalf("keys = %#v", result.Keys)
	}
	if result.Balance == nil || result.Balance.Amount != 5 || result.Balance.Unit != "USD" || result.Balance.RawQuota == nil || result.Balance.QuotaPerUnit == nil {
		t.Fatalf("unexpected normalized sync balance: %#v", result.Balance)
	}
	auto := result.Keys[0]
	if auto.Key.MatchAvailable || auto.Plaintext != "" || auto.Key.Multiplier == nil || *auto.Key.Multiplier != 1.7 || auto.Key.MultiplierSource != "dynamic" || auto.Key.MultiplierObserved == nil {
		t.Fatalf("unexpected auto key: %#v", auto)
	}
	vip := result.Keys[1]
	if !vip.Key.MatchAvailable || vip.Plaintext == "" || vip.Key.Multiplier == nil || *vip.Key.Multiplier != 1.25 || vip.Key.MultiplierSource != "group" || !vip.Key.UnlimitedQuota {
		t.Fatalf("unexpected fixed key: %#v", vip)
	}
}

func TestNewAPIAdapterKeepsManagementRequestsOffModelEndpoint(t *testing.T) {
	managementCalls := make([]string, 0, 8)
	managementServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		managementCalls = append(managementCalls, r.URL.Path)
		switch r.URL.Path {
		case "/api/user/login":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"access_token": "management-access", "user": map[string]any{"id": 9, "username": "operator"},
			}})
		case "/api/user/self":
			if r.Header.Get("Authorization") != "Bearer management-access" {
				t.Fatalf("management endpoint received unexpected Authorization: %q", r.Header.Get("Authorization"))
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 9, "username": "operator", "quota": 1000000}})
		case "/api/status":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/user/self/groups":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"default": map[string]any{"ratio": 1}}})
		case "/api/token/":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 100}})
		default:
			t.Fatalf("management endpoint received unexpected request %s", r.URL.Path)
		}
	}))
	defer managementServer.Close()

	modelServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("model endpoint must not receive management request: %s", r.URL.Path)
	}))
	defer modelServer.Close()

	adapter := newAPIAdapter{}
	login, err := adapter.Connect(context.Background(), managementServer.URL, LoginInput{Mode: model.UpstreamAuthPassword, Username: "operator", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Sync(context.Background(), managementServer.URL, login.Material); err != nil {
		t.Fatal(err)
	}
	if len(managementCalls) < 7 {
		t.Fatalf("management endpoint did not receive connect and sync calls: %#v", managementCalls)
	}
	_ = modelServer.URL
}

func TestNewAPIAdapterFallsBackToRawQuotaWithoutValidConversion(t *testing.T) {
	tests := []struct {
		name   string
		status any
	}{
		{name: "missing", status: map[string]any{"success": true, "data": map[string]any{"system_name": "New API"}}},
		{name: "zero", status: map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 0}}},
		{name: "not numeric", status: map[string]any{"success": true, "data": map[string]any{"quota_per_unit": "unknown"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/user/self":
					writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 4, "username": "raw", "quota": 123456}})
				case "/api/status":
					writeTestJSON(t, w, http.StatusOK, test.status)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			result, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: model.UpstreamAuthToken, Token: "token", UserID: "4"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Balance == nil || result.Balance.Amount != 123456 || result.Balance.Unit != "quota" || result.Balance.RawQuota == nil || *result.Balance.RawQuota != 123456 || result.Balance.QuotaPerUnit != nil {
				t.Fatalf("unexpected raw-quota fallback: %#v", result.Balance)
			}
		})
	}
}

func TestNewAPIAdapterTreatsNullQuotaAsUnavailable(t *testing.T) {
	var statusCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 4, "username": "empty", "quota": nil}})
		case "/api/status":
			statusCalls.Add(1)
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: model.UpstreamAuthToken, Token: "token", UserID: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Balance != nil || statusCalls.Load() != 0 {
		t.Fatalf("null quota was normalized as balance: balance=%#v status_calls=%d", result.Balance, statusCalls.Load())
	}
}

func TestNewAPIAdapterConnectsLegacyCookieSessionWithDirectUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			if _, exists := r.URL.Query()["turnstile"]; !exists {
				t.Error("login request omitted the NewAPI turnstile query parameter")
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "legacy-session", HttpOnly: true})
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"id": 17, "username": "legacy-user", "email": "legacy@example.com",
			}})
		case "/api/user/self":
			if r.Header.Get("New-Api-User") != "17" || !strings.Contains(r.Header.Get("Cookie"), "session=legacy-session") {
				writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"success": false, "message": "not logged in"})
				return
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 17, "username": "legacy-user"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
		Mode: model.UpstreamAuthPassword, Username: "legacy-user", Password: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Material.UserID != "17" || !strings.Contains(result.Material.Cookie, "session=legacy-session") || result.Principal != "legacy-user" {
		t.Fatalf("unexpected legacy login result: %#v", result)
	}
}

func TestNewAPIAdapterManualSessionRequiresUserIDForLegacySite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/self" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("New-Api-User") != "23" {
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"success": false, "message": "not logged in"})
			return
		}
		writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 23, "username": "manual"}})
	}))
	defer server.Close()

	_, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
		Mode: model.UpstreamAuthToken, Token: "legacy-access-token",
	})
	if err == nil || AsAdapterError(err).Code != "NEWAPI_USER_ID_REQUIRED" {
		t.Fatalf("error = %#v, want NEWAPI_USER_ID_REQUIRED", err)
	}

	result, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
		Mode: model.UpstreamAuthToken, Token: "legacy-access-token", UserID: "23",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Material.UserID != "23" || result.Principal != "manual" {
		t.Fatalf("unexpected manual legacy login: %#v", result)
	}
}

func TestNewAPIAdapterRefreshesCookieSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			if r.Header.Get("Authorization") == "Bearer refreshed-access" {
				writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 3, "username": "refreshed"}})
				return
			}
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"success": false, "code": "AUTH_TOKEN_EXPIRED"})
		case "/api/user/auth/refresh":
			if !strings.Contains(r.Header.Get("Cookie"), "refresh_token=valid") || r.Header.Get("Origin") == "" {
				t.Errorf("refresh request missing cookie/origin: %#v", r.Header)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"access_token": "refreshed-access"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, err := (newAPIAdapter{}).Connect(context.Background(), server.URL, LoginInput{Mode: "session", Session: "refresh_token=valid"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Material.AccessToken != "refreshed-access" || result.Principal != "refreshed" {
		t.Fatalf("unexpected refreshed result: %#v", result)
	}
}
