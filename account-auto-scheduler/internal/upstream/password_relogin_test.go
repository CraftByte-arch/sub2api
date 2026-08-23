package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestCredentialBoxProtectsPasswordReloginMaterial(t *testing.T) {
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	material := AuthMaterial{
		AccessToken:   "access-secret",
		RefreshToken:  "refresh-secret",
		Cookie:        "session=cookie-secret",
		UserID:        "7",
		LoginUsername: "operator@example.com",
		LoginPassword: "password-secret",
	}
	envelope, err := box.Encrypt("upstream", "identity", material)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{material.LoginUsername, material.LoginPassword} {
		if strings.Contains(envelope.Ciphertext, plaintext) {
			t.Fatalf("credential ciphertext contains password login material %q", plaintext)
		}
	}
	decrypted, err := box.Decrypt("upstream", "identity", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != material {
		t.Fatalf("decrypted material = %#v, want %#v", decrypted, material)
	}
	input, ok := decrypted.passwordLoginInput()
	if !ok || input.Mode != model.UpstreamAuthPassword || input.Username != material.LoginUsername || input.Password != material.LoginPassword {
		t.Fatalf("password login input = %#v ok=%t", input, ok)
	}

	legacyEnvelope, err := box.Encrypt("upstream", "legacy", AuthMaterial{AccessToken: "legacy-access"})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := box.Decrypt("upstream", "legacy", legacyEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := legacy.passwordLoginInput(); ok {
		t.Fatalf("legacy material unexpectedly enabled password re-login: %#v", legacy)
	}
}

func TestSub2APIAutomaticPasswordReloginAfterRefreshFailure(t *testing.T) {
	var refreshCalls atomic.Int64
	var loginCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") == "Bearer new-access" {
				writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 7, "email": "operator@example.com", "balance": 9}})
				return
			}
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED", "message": "expired"})
		case "/api/v1/auth/refresh":
			refreshCalls.Add(1)
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED", "message": "expired"})
		case sub2APICredentialKeyRoute:
			http.NotFound(w, r)
		case "/api/v1/auth/login":
			loginCalls.Add(1)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["email"] != "operator@example.com" || body["password"] != "password-secret" {
				t.Fatalf("unexpected automatic login body: %#v", body)
			}
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "new-access", "refresh_token": "new-refresh", "user": map[string]any{"id": 7, "email": "operator@example.com"},
			}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{
		AccessToken: "old-access", RefreshToken: "old-refresh",
		LoginUsername: "operator@example.com", LoginPassword: "password-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalls.Load() != 1 || loginCalls.Load() != 1 {
		t.Fatalf("refresh_calls=%d login_calls=%d, want 1 and 1", refreshCalls.Load(), loginCalls.Load())
	}
	if result.Material.AccessToken != "new-access" || result.Material.RefreshToken != "new-refresh" || result.Material.LoginUsername != "operator@example.com" || result.Material.LoginPassword != "password-secret" {
		t.Fatalf("unexpected re-login material: %#v", result.Material)
	}
	if result.Balance == nil || result.Balance.Amount != 9 {
		t.Fatalf("sync did not continue after re-login: %#v", result)
	}
}

func TestSub2APIAutomaticReloginPrefersRefresh(t *testing.T) {
	var refreshCalls atomic.Int64
	var loginCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") == "Bearer refreshed-access" {
				writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 1}})
				return
			}
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED"})
		case "/api/v1/auth/refresh":
			refreshCalls.Add(1)
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"access_token": "refreshed-access", "refresh_token": "refreshed-refresh"}})
		case "/api/v1/auth/login":
			loginCalls.Add(1)
			http.Error(w, "must not login", http.StatusInternalServerError)
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{
		AccessToken: "old-access", RefreshToken: "old-refresh",
		LoginUsername: "operator@example.com", LoginPassword: "password-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalls.Load() != 1 || loginCalls.Load() != 0 || result.Material.AccessToken != "refreshed-access" || result.Material.LoginPassword != "password-secret" {
		t.Fatalf("unexpected refresh-first result: material=%#v refresh_calls=%d login_calls=%d", result.Material, refreshCalls.Load(), loginCalls.Load())
	}
}

func TestSub2APIAutomaticReloginPreservesFailureBoundaries(t *testing.T) {
	t.Run("legacy material", func(t *testing.T) {
		var loginCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == sub2APICredentialKeyRoute {
				loginCalls.Add(1)
			}
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED"})
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{AccessToken: "legacy-access"})
		if AsAdapterError(err).Code != "UPSTREAM_SESSION_EXPIRED" || loginCalls.Load() != 0 {
			t.Fatalf("error=%#v login_calls=%d", err, loginCalls.Load())
		}
	})

	t.Run("captcha", func(t *testing.T) {
		var loginCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/me":
				writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED"})
			case sub2APICredentialKeyRoute:
				http.NotFound(w, r)
			case "/api/v1/auth/login":
				loginCalls.Add(1)
				writeTestJSON(t, w, http.StatusOK, map[string]any{"code": "CAPTCHA_REQUIRED", "message": "captcha required"})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{
			AccessToken: "expired", LoginUsername: "operator@example.com", LoginPassword: "password-secret",
		})
		if failure := AsAdapterError(err); failure.Status != model.IdentityStatusCaptcha || loginCalls.Load() != 1 {
			t.Fatalf("error=%#v login_calls=%d", failure, loginCalls.Load())
		}
	})

	t.Run("management access denied", func(t *testing.T) {
		var loginCalls atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == sub2APICredentialKeyRoute {
				loginCalls.Add(1)
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("blocked"))
		}))
		defer server.Close()
		_, err := (sub2APIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{
			AccessToken: "blocked", LoginUsername: "operator@example.com", LoginPassword: "password-secret",
		})
		if failure := AsAdapterError(err); failure.Status != model.IdentityStatusAccessDenied || loginCalls.Load() != 0 {
			t.Fatalf("error=%#v login_calls=%d", failure, loginCalls.Load())
		}
	})
}

func TestNewAPIAutomaticPasswordReloginAfterRefreshFailure(t *testing.T) {
	var refreshCalls atomic.Int64
	var loginCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			if r.Header.Get("Authorization") == "Bearer new-access" {
				writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 9, "username": "operator", "quota": 500000}})
				return
			}
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"success": false, "code": "AUTH_TOKEN_EXPIRED"})
		case "/api/user/auth/refresh":
			refreshCalls.Add(1)
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"success": false, "code": "AUTH_TOKEN_EXPIRED"})
		case "/api/user/login":
			loginCalls.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["username"] != "operator" || body["password"] != "password-secret" {
				t.Fatalf("unexpected automatic login body: %#v", body)
			}
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "new-cookie", HttpOnly: true})
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"access_token": "new-access", "user": map[string]any{"id": 9, "username": "operator"},
			}})
		case "/api/status":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"quota_per_unit": 500000}})
		case "/api/user/self/groups":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{}})
		case "/api/token/":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 100}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (newAPIAdapter{}).Sync(context.Background(), server.URL, AuthMaterial{
		AccessToken: "old-access", Cookie: "refresh_token=old-cookie", UserID: "9",
		LoginUsername: "operator", LoginPassword: "password-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalls.Load() != 1 || loginCalls.Load() != 1 {
		t.Fatalf("refresh_calls=%d login_calls=%d, want 1 and 1", refreshCalls.Load(), loginCalls.Load())
	}
	if result.Material.AccessToken != "new-access" || !strings.Contains(result.Material.Cookie, "refresh_token=new-cookie") || result.Material.UserID != "9" || result.Material.LoginUsername != "operator" || result.Material.LoginPassword != "password-secret" {
		t.Fatalf("unexpected NewAPI re-login material: %#v", result.Material)
	}
	if result.Balance == nil || result.Balance.Amount != 1 {
		t.Fatalf("NewAPI sync did not continue after re-login: %#v", result)
	}
}

func TestManagerPersistsAutomaticPasswordReloginMaterial(t *testing.T) {
	var loginCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") == "Bearer persisted-access" {
				writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"id": 4, "email": "persist@example.com", "balance": 3}})
				return
			}
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED"})
		case "/api/v1/auth/refresh":
			writeTestJSON(t, w, http.StatusUnauthorized, map[string]any{"code": "TOKEN_EXPIRED"})
		case sub2APICredentialKeyRoute:
			http.NotFound(w, r)
		case "/api/v1/auth/login":
			loginCalls.Add(1)
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{
				"access_token": "persisted-access", "refresh_token": "persisted-refresh", "user": map[string]any{"id": 4, "email": "persist@example.com"},
			}})
		case "/api/v1/groups/available":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": []any{}})
		case "/api/v1/groups/rates":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/v1/keys":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "pages": 1}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	baseURL, err := model.NormalizeUpstreamBaseURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstreamID := model.StableUpstreamID(baseURL)
	oldMaterial := AuthMaterial{
		AccessToken: "expired-access", RefreshToken: "expired-refresh",
		LoginUsername: "persist@example.com", LoginPassword: "persist-password",
	}
	oldEnvelope, err := box.Encrypt(upstreamID, "identity", oldMaterial)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stateStore := managerTestStore(t)
	if err := stateStore.PutUpstream(model.ManagedUpstream{
		ID: upstreamID, Name: "Persist", BaseURL: baseURL, TypeOverride: model.UpstreamTypeSub2API,
		Detection: model.UpstreamDetection{Type: model.UpstreamTypeSub2API}, CreatedAt: now, UpdatedAt: now,
		Identities: map[string]model.UpstreamIdentity{"identity": {
			ID: "identity", Label: "Primary", AuthMode: model.UpstreamAuthPassword,
			Credential: oldEnvelope, Status: model.IdentityStatusConnected, Keys: map[string]model.RemoteKey{}, CreatedAt: now, UpdatedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(stateStore, &managerLocalCore{}, box, 0, nil)
	if err := manager.SyncIdentity(context.Background(), upstreamID, "identity"); err != nil {
		t.Fatal(err)
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("automatic login calls = %d, want 1", loginCalls.Load())
	}
	stored, err := stateStore.GetUpstream(upstreamID)
	if err != nil {
		t.Fatal(err)
	}
	identity := stored.Identities["identity"]
	if identity.Credential == oldEnvelope || identity.Status != model.IdentityStatusConnected || identity.Balance == nil || identity.Balance.Amount != 3 {
		t.Fatalf("automatic re-login result was not persisted: %#v", identity)
	}
	decrypted, err := box.Decrypt(upstreamID, "identity", identity.Credential)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.AccessToken != "persisted-access" || decrypted.RefreshToken != "persisted-refresh" || decrypted.LoginUsername != "persist@example.com" || decrypted.LoginPassword != "persist-password" {
		t.Fatalf("unexpected persisted credential material: %#v", decrypted)
	}
	publicView, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publicJSON, err := json.Marshal(publicView)
	if err != nil {
		t.Fatal(err)
	}
	storedJSON, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{"persist-password", "persisted-access", "persisted-refresh", "login_username", "login_password"} {
		if strings.Contains(string(publicJSON), plaintext) || strings.Contains(string(storedJSON), plaintext) {
			t.Fatalf("plaintext credential %q leaked: public=%s stored=%s", plaintext, publicJSON, storedJSON)
		}
	}
}
