package upstream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestSub2APICredentialEnvelopeLoginMatchesBrowserProtocol(t *testing.T) {
	privateKey, publicKey := testCredentialFlowRSAKey(t, 2048)
	for _, withCaptcha := range []bool{false, true} {
		name := "without captcha"
		if withCaptcha {
			name = "with captcha"
		}
		t.Run(name, func(t *testing.T) {
			serverTime := time.Now().Unix() + 3600
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case sub2APICredentialKeyRoute:
					if withCaptcha && !strings.Contains(r.Header.Get("Cookie"), "captcha_session=server") {
						t.Fatalf("credential discovery omitted captcha cookie: %q", r.Header.Get("Cookie"))
					}
					http.SetCookie(w, &http.Cookie{Name: "sub2api_auth_flow", Value: "flow", HttpOnly: true})
					http.SetCookie(w, &http.Cookie{Name: "flow_aux", Value: "aux", HttpOnly: true})
					writeCredentialKeyResponse(t, w, http.StatusOK, sub2APICredentialKeyResponse{
						Algorithm:     sub2APICredentialEnvelopeAlgorithm,
						KeyID:         "flow-key",
						PublicKey:     publicKey,
						ServerTime:    serverTime,
						ExpiresAt:     serverTime + 3600,
						FlowExpiresAt: serverTime + 900,
					})
				case "/api/v1/auth/login":
					cookie := r.Header.Get("Cookie")
					if !strings.Contains(cookie, "sub2api_auth_flow=flow") || !strings.Contains(cookie, "flow_aux=aux") {
						t.Fatalf("login omitted credential-flow cookies: %q", cookie)
					}
					if withCaptcha && !strings.Contains(cookie, "captcha_session=server") {
						t.Fatalf("login omitted captcha cookie: %q", cookie)
					}
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if _, exists := body["email"]; exists {
						t.Fatalf("secure login leaked top-level email: %s", body["email"])
					}
					if _, exists := body["password"]; exists {
						t.Fatal("secure login leaked top-level password")
					}
					if withCaptcha {
						assertRawJSONString(t, body["captcha_id"], "captcha-id")
						assertRawJSONString(t, body["captcha_code"], "AB12")
					} else if _, exists := body["captcha_id"]; exists {
						t.Fatalf("captcha-free secure login sent captcha fields: %#v", body)
					}
					var envelope sub2APICredentialEnvelope
					if err := json.Unmarshal(body["credential_envelope"], &envelope); err != nil {
						t.Fatal(err)
					}
					plaintext := decryptCredentialEnvelope(t, privateKey, envelope)
					if plaintext.Email != "operator@example.com" || plaintext.Password != "password" {
						t.Fatalf("decrypted credential payload mismatch: %#v", plaintext)
					}
					if plaintext.IssuedAt < serverTime || plaintext.IssuedAt > serverTime+3 {
						t.Fatalf("issued_at=%d, want upstream-adjusted time near %d", plaintext.IssuedAt, serverTime)
					}
					http.SetCookie(w, &http.Cookie{Name: "sub2api_auth_flow", Value: "rotated", HttpOnly: true})
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

			input := LoginInput{Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "password"}
			if withCaptcha {
				input.CaptchaID = "captcha-id"
				input.CaptchaCode = "ab12"
				input.ChallengeCookie = "captcha_session=server"
			}
			result, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, input)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.Material.Cookie, "sub2api_auth_flow=") || strings.Contains(result.Material.Cookie, "flow_aux=") {
				t.Fatalf("ephemeral credential-flow cookie became durable: %q", result.Material.Cookie)
			}
			if !strings.Contains(result.Material.Cookie, "session=final") {
				t.Fatalf("durable login cookie was lost: %q", result.Material.Cookie)
			}
			if withCaptcha && !strings.Contains(result.Material.Cookie, "captcha_session=server") {
				t.Fatalf("existing captcha cookie behavior changed: %q", result.Material.Cookie)
			}
		})
	}
}

func TestSub2APICredentialFlowFallsBackOnlyForExplicitLegacyResponses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "not found", status: http.StatusNotFound, contentType: "application/json", body: `{"message":"not found"}`},
		{name: "method not allowed", status: http.StatusMethodNotAllowed, contentType: "application/json", body: `{"message":"method not allowed"}`},
		{name: "spa html", status: http.StatusOK, contentType: "text/html; charset=utf-8", body: `<!doctype html><html><body>app</body></html>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var loginCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case sub2APICredentialKeyRoute:
					w.Header().Set("Content-Type", test.contentType)
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.body))
				case "/api/v1/auth/login":
					loginCalls.Add(1)
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					assertRawJSONString(t, body["email"], "operator@example.com")
					assertRawJSONString(t, body["password"], "password")
					if _, exists := body["credential_envelope"]; exists {
						t.Fatalf("legacy login unexpectedly sent an envelope: %#v", body)
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
			if loginCalls.Load() != 1 {
				t.Fatalf("legacy login calls=%d, want 1", loginCalls.Load())
			}
		})
	}
}

func TestSub2APICredentialFlowFailsClosed(t *testing.T) {
	_, validPublicKey := testCredentialFlowRSAKey(t, 2048)
	_, unsafePublicKey := testCredentialFlowRSAKey(t, 1024)
	serverTime := time.Now().Unix()
	tests := []struct {
		name      string
		status    int
		body      any
		content   string
		setCookie bool
		wantCode  string
	}{
		{name: "unsupported algorithm", status: http.StatusOK, setCookie: true, body: sub2APICredentialKeyResponse{Algorithm: "future", KeyID: "key", PublicKey: validPublicKey, ServerTime: serverTime, ExpiresAt: serverTime + 3600, FlowExpiresAt: serverTime + 900}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_UNSUPPORTED"},
		{name: "malformed key", status: http.StatusOK, setCookie: true, body: sub2APICredentialKeyResponse{Algorithm: sub2APICredentialEnvelopeAlgorithm, KeyID: "key", PublicKey: "not-base64", ServerTime: serverTime, ExpiresAt: serverTime + 3600, FlowExpiresAt: serverTime + 900}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_INVALID_KEY"},
		{name: "unsafe key", status: http.StatusOK, setCookie: true, body: sub2APICredentialKeyResponse{Algorithm: sub2APICredentialEnvelopeAlgorithm, KeyID: "key", PublicKey: unsafePublicKey, ServerTime: serverTime, ExpiresAt: serverTime + 3600, FlowExpiresAt: serverTime + 900}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_INVALID_KEY"},
		{name: "expired flow", status: http.StatusOK, setCookie: true, body: sub2APICredentialKeyResponse{Algorithm: sub2APICredentialEnvelopeAlgorithm, KeyID: "key", PublicKey: validPublicKey, ServerTime: serverTime, ExpiresAt: serverTime + 3600, FlowExpiresAt: serverTime}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_EXPIRED"},
		{name: "missing flow cookie", status: http.StatusOK, body: sub2APICredentialKeyResponse{Algorithm: sub2APICredentialEnvelopeAlgorithm, KeyID: "key", PublicKey: validPublicKey, ServerTime: serverTime, ExpiresAt: serverTime + 3600, FlowExpiresAt: serverTime + 900}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_INVALID"},
		{name: "forbidden", status: http.StatusForbidden, body: map[string]any{"message": "denied"}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_DENIED"},
		{name: "rate limited", status: http.StatusTooManyRequests, body: map[string]any{"message": "slow down"}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_RATE_LIMITED"},
		{name: "server failure", status: http.StatusBadGateway, body: map[string]any{"message": "unavailable"}, wantCode: "UPSTREAM_CREDENTIAL_FLOW_UNAVAILABLE"},
		{name: "malformed success", status: http.StatusOK, content: `{"code":0,"data":`, wantCode: "UPSTREAM_CREDENTIAL_FLOW_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var loginCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case sub2APICredentialKeyRoute:
					if test.setCookie {
						http.SetCookie(w, &http.Cookie{Name: "sub2api_auth_flow", Value: "flow", HttpOnly: true})
					}
					if test.content != "" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(test.status)
						_, _ = w.Write([]byte(test.content))
						return
					}
					writeCredentialKeyResponse(t, w, test.status, test.body)
				case "/api/v1/auth/login":
					loginCalls.Add(1)
					writeTestJSON(t, w, http.StatusInternalServerError, map[string]any{"message": "must not be called"})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			_, err := (sub2APIAdapter{}).Connect(context.Background(), server.URL, LoginInput{
				Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "password",
			})
			if failure := AsAdapterError(err); failure.Code != test.wantCode {
				t.Fatalf("error=%#v, want code %s", failure, test.wantCode)
			}
			if loginCalls.Load() != 0 {
				t.Fatalf("secure discovery failure downgraded to %d login calls", loginCalls.Load())
			}
		})
	}

	t.Run("network failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		baseURL := server.URL
		server.Close()
		_, err := (sub2APIAdapter{}).Connect(context.Background(), baseURL, LoginInput{
			Mode: model.UpstreamAuthPassword, Username: "operator@example.com", Password: "password",
		})
		if failure := AsAdapterError(err); failure.Code != "UPSTREAM_CREDENTIAL_FLOW_UNAVAILABLE" {
			t.Fatalf("network error=%#v", failure)
		}
	})
}

func TestSub2APICredentialFlowProbeIsScopedToPasswordMode(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()
	client, err := newRemoteClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sub2APILoginMaterial(context.Background(), client, LoginInput{Mode: model.UpstreamAuthToken, Token: "token"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sub2APILoginMaterial(context.Background(), client, LoginInput{Mode: model.UpstreamAuthSession, Session: "session=value"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("non-password Sub2API modes made %d requests", calls.Load())
	}

	newAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
				"access_token": "access", "user": map[string]any{"id": 9, "username": "operator"},
			}})
		case "/api/user/self":
			writeTestJSON(t, w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"id": 9, "username": "operator"}})
		default:
			t.Fatalf("NewAPI received unexpected credential-flow request: %s", r.URL.Path)
		}
	}))
	defer newAPIServer.Close()
	if _, err := (newAPIAdapter{}).Connect(context.Background(), newAPIServer.URL, LoginInput{
		Mode: model.UpstreamAuthPassword, Username: "operator", Password: "password",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSub2APICredentialPublicKeyAcceptsBase64SPKIAndPEM(t *testing.T) {
	privateKey, encoded := testCredentialFlowRSAKey(t, 2048)
	if parsed, err := parseSub2APICredentialPublicKey(encoded); err != nil || parsed.N.Cmp(privateKey.PublicKey.N) != 0 {
		t.Fatalf("base64 SPKI parse failed: key=%#v err=%v", parsed, err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pemValue := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if parsed, err := parseSub2APICredentialPublicKey(string(pemValue)); err != nil || parsed.N.Cmp(privateKey.PublicKey.N) != 0 {
		t.Fatalf("PEM SPKI parse failed: key=%#v err=%v", parsed, err)
	}
}

func TestClassifyLoginErrorRecognizesBrowserCredentialFlow(t *testing.T) {
	failure := classifyLoginError(http.StatusForbidden, []byte(`{"code":403,"message":"browser credential flow is required"}`))
	if failure.Code != "UPSTREAM_CREDENTIAL_FLOW_REQUIRED" || failure.Status == model.IdentityStatusAccessDenied {
		t.Fatalf("credential-flow rejection was misclassified: %#v", failure)
	}
}

func writeCredentialKeyResponse(t *testing.T, w http.ResponseWriter, status int, data any) {
	t.Helper()
	writeTestJSON(t, w, status, map[string]any{"code": 0, "message": "success", "data": data})
}

func testCredentialFlowRSAKey(t *testing.T, bits int) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, base64.StdEncoding.EncodeToString(der)
}

func decryptCredentialEnvelope(t *testing.T, privateKey *rsa.PrivateKey, envelope sub2APICredentialEnvelope) sub2APICredentialPlaintext {
	t.Helper()
	if envelope.Algorithm != sub2APICredentialEnvelopeAlgorithm || envelope.KeyID != "flow-key" {
		t.Fatalf("unexpected envelope metadata: %#v", envelope)
	}
	encryptedKey, err := base64.RawURLEncoding.DecodeString(envelope.EncryptedKey)
	if err != nil {
		t.Fatal(err)
	}
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, encryptedKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := base64.RawURLEncoding.DecodeString(envelope.IV)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	plaintextJSON, err := gcm.Open(nil, iv, ciphertext, []byte(envelope.KeyID))
	if err != nil {
		t.Fatal(err)
	}
	var plaintext sub2APICredentialPlaintext
	if err := json.Unmarshal(plaintextJSON, &plaintext); err != nil {
		t.Fatal(err)
	}
	return plaintext
}

func assertRawJSONString(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value != want {
		t.Fatalf("JSON string=%q err=%v, want %q", value, err, want)
	}
}
