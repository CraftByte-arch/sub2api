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
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	sub2APICredentialEnvelopeAlgorithm = "RSA-OAEP-256+A256GCM"
	sub2APICredentialKeyRoute          = "/api/v1/auth/credential-key"
	sub2APICredentialAESKeySize        = 32
	sub2APICredentialGCMNonceSize      = 12
	sub2APICredentialMinRSAKeyBits     = 2048
	sub2APICredentialMaxRSAKeyBits     = 8192
	sub2APICredentialMaxPublicKeyText  = 16 << 10
)

type sub2APICredentialKeyResponse struct {
	Algorithm     string `json:"algorithm"`
	KeyID         string `json:"key_id"`
	PublicKey     string `json:"public_key"`
	ServerTime    int64  `json:"server_time"`
	ExpiresAt     int64  `json:"expires_at"`
	FlowExpiresAt int64  `json:"flow_expires_at"`
}

type sub2APICredentialFlow struct {
	Algorithm        string
	KeyID            string
	PublicKey        *rsa.PublicKey
	ServerTimeOffset int64
	ExpiresAt        int64
	FlowExpiresAt    int64
	Cookie           string
	CookieNames      []string
}

type sub2APICredentialEnvelope struct {
	Algorithm    string `json:"algorithm"`
	KeyID        string `json:"key_id"`
	EncryptedKey string `json:"encrypted_key"`
	IV           string `json:"iv"`
	Ciphertext   string `json:"ciphertext"`
}

type sub2APICredentialPlaintext struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	IssuedAt int64  `json:"issued_at"`
}

func discoverSub2APICredentialFlow(
	ctx context.Context,
	client *remoteClient,
	material AuthMaterial,
	now time.Time,
) (sub2APICredentialFlow, bool, error) {
	response, err := client.do(ctx, http.MethodGet, sub2APICredentialKeyRoute, nil, material)
	if err != nil {
		return sub2APICredentialFlow{}, false, adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_UNAVAILABLE",
			"无法获取上游加密登录材料，请稍后重试",
			model.IdentityStatusNetworkError,
			http.StatusBadGateway,
		)
	}
	if isExplicitLegacyCredentialFlowResponse(response) {
		return sub2APICredentialFlow{}, false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return sub2APICredentialFlow{}, false, credentialFlowStatusError(response.StatusCode)
	}

	var keyResponse sub2APICredentialKeyResponse
	if err := decodeSub2APIResponse(response, &keyResponse); err != nil {
		return sub2APICredentialFlow{}, false, adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_INVALID",
			"上游返回了无效的加密登录材料",
			model.IdentityStatusSyncError,
			http.StatusBadGateway,
		)
	}
	if strings.TrimSpace(keyResponse.Algorithm) != sub2APICredentialEnvelopeAlgorithm {
		return sub2APICredentialFlow{}, false, adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_UNSUPPORTED",
			"上游要求不受支持的加密登录协议",
			model.IdentityStatusSyncError,
			http.StatusBadGateway,
		)
	}
	keyID := strings.TrimSpace(keyResponse.KeyID)
	if keyID == "" || len(keyID) > maxCaptchaIDLength || containsControlCharacter(keyID) {
		return sub2APICredentialFlow{}, false, invalidCredentialFlowKeyError()
	}
	publicKey, err := parseSub2APICredentialPublicKey(keyResponse.PublicKey)
	if err != nil {
		return sub2APICredentialFlow{}, false, invalidCredentialFlowKeyError()
	}
	if keyResponse.ServerTime <= 0 || keyResponse.ExpiresAt <= keyResponse.ServerTime || keyResponse.FlowExpiresAt <= keyResponse.ServerTime {
		return sub2APICredentialFlow{}, false, expiredCredentialFlowError()
	}
	cookie := cleanHeaderValue(joinResponseCookies(response.Header))
	if cookie == "" {
		return sub2APICredentialFlow{}, false, adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_INVALID",
			"上游加密登录材料缺少流程 Cookie",
			model.IdentityStatusSyncError,
			http.StatusBadGateway,
		)
	}
	if now.IsZero() {
		now = time.Now()
	}
	return sub2APICredentialFlow{
		Algorithm:        sub2APICredentialEnvelopeAlgorithm,
		KeyID:            keyID,
		PublicKey:        publicKey,
		ServerTimeOffset: keyResponse.ServerTime - now.Unix(),
		ExpiresAt:        keyResponse.ExpiresAt,
		FlowExpiresAt:    keyResponse.FlowExpiresAt,
		Cookie:           cookie,
		CookieNames:      cookieMaterialNames(cookie),
	}, true, nil
}

func buildSub2APICredentialEnvelope(
	flow sub2APICredentialFlow,
	email string,
	password string,
	now time.Time,
	random io.Reader,
) (sub2APICredentialEnvelope, error) {
	if flow.PublicKey == nil || flow.Algorithm != sub2APICredentialEnvelopeAlgorithm || flow.KeyID == "" {
		return sub2APICredentialEnvelope{}, invalidCredentialFlowKeyError()
	}
	if now.IsZero() {
		now = time.Now()
	}
	serverNow := now.Unix() + flow.ServerTimeOffset
	if flow.ExpiresAt <= serverNow || flow.FlowExpiresAt <= serverNow {
		return sub2APICredentialEnvelope{}, expiredCredentialFlowError()
	}
	if random == nil {
		random = rand.Reader
	}

	plaintext, err := json.Marshal(sub2APICredentialPlaintext{
		Email:    email,
		Password: password,
		IssuedAt: serverNow,
	})
	if err != nil {
		return sub2APICredentialEnvelope{}, credentialEncryptionError()
	}
	defer clearSensitiveBytes(plaintext)

	aesKey := make([]byte, sub2APICredentialAESKeySize)
	defer clearSensitiveBytes(aesKey)
	if _, err := io.ReadFull(random, aesKey); err != nil {
		return sub2APICredentialEnvelope{}, credentialEncryptionError()
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return sub2APICredentialEnvelope{}, credentialEncryptionError()
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, sub2APICredentialGCMNonceSize)
	if err != nil {
		return sub2APICredentialEnvelope{}, credentialEncryptionError()
	}
	iv := make([]byte, sub2APICredentialGCMNonceSize)
	if _, err := io.ReadFull(random, iv); err != nil {
		return sub2APICredentialEnvelope{}, credentialEncryptionError()
	}
	ciphertext := gcm.Seal(nil, iv, plaintext, []byte(flow.KeyID))
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), random, flow.PublicKey, aesKey, nil)
	if err != nil {
		return sub2APICredentialEnvelope{}, credentialEncryptionError()
	}

	return sub2APICredentialEnvelope{
		Algorithm:    sub2APICredentialEnvelopeAlgorithm,
		KeyID:        flow.KeyID,
		EncryptedKey: base64.RawURLEncoding.EncodeToString(encryptedKey),
		IV:           base64.RawURLEncoding.EncodeToString(iv),
		Ciphertext:   base64.RawURLEncoding.EncodeToString(ciphertext),
	}, nil
}

func parseSub2APICredentialPublicKey(value string) (*rsa.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > sub2APICredentialMaxPublicKeyText {
		return nil, errors.New("invalid credential public key")
	}
	der := []byte(nil)
	if block, _ := pem.Decode([]byte(value)); block != nil {
		if block.Type != "PUBLIC KEY" {
			return nil, errors.New("credential public key is not SPKI")
		}
		der = block.Bytes
	} else {
		compact := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
				return -1
			}
			return r
		}, value)
		encodings := []*base64.Encoding{
			base64.StdEncoding,
			base64.RawStdEncoding,
			base64.URLEncoding,
			base64.RawURLEncoding,
		}
		for _, encoding := range encodings {
			decoded, err := encoding.DecodeString(compact)
			if err == nil {
				der = decoded
				break
			}
		}
		if len(der) == 0 {
			return nil, errors.New("credential public key is not base64")
		}
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.New("credential public key is not SPKI")
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok || publicKey.N == nil || publicKey.E < 3 {
		return nil, errors.New("credential public key is not RSA")
	}
	bits := publicKey.N.BitLen()
	if bits < sub2APICredentialMinRSAKeyBits || bits > sub2APICredentialMaxRSAKeyBits {
		return nil, errors.New("credential RSA key size is unsafe")
	}
	return publicKey, nil
}

func isExplicitLegacyCredentialFlowResponse(response remoteResponse) bool {
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		return true
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	prefix := strings.ToLower(strings.TrimSpace(string(response.Body)))
	if len(prefix) > 2048 {
		prefix = prefix[:2048]
	}
	looksLikeDocument := strings.HasPrefix(prefix, "<!doctype html") ||
		strings.HasPrefix(prefix, "<html") ||
		strings.Contains(prefix, "<html")
	return looksLikeDocument && (strings.Contains(contentType, "text/html") || strings.HasPrefix(prefix, "<"))
}

func credentialFlowStatusError(statusCode int) *AdapterError {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_DENIED",
			"上游拒绝提供加密登录材料，请在上游完成验证后重试",
			model.IdentityStatusAccessDenied,
			statusCode,
		)
	case http.StatusTooManyRequests:
		return adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_RATE_LIMITED",
			"上游暂时限制了加密登录请求，请稍后重试",
			model.IdentityStatusSyncError,
			statusCode,
		)
	default:
		return adapterError(
			"UPSTREAM_CREDENTIAL_FLOW_UNAVAILABLE",
			"上游加密登录材料暂不可用，请稍后重试",
			model.IdentityStatusNetworkError,
			http.StatusBadGateway,
		)
	}
}

func invalidCredentialFlowKeyError() *AdapterError {
	return adapterError(
		"UPSTREAM_CREDENTIAL_FLOW_INVALID_KEY",
		"上游返回了无效的加密登录公钥",
		model.IdentityStatusSyncError,
		http.StatusBadGateway,
	)
}

func expiredCredentialFlowError() *AdapterError {
	return adapterError(
		"UPSTREAM_CREDENTIAL_FLOW_EXPIRED",
		"上游加密登录材料已过期，请重新登录",
		model.IdentityStatusExpired,
		http.StatusConflict,
	)
}

func credentialEncryptionError() *AdapterError {
	return adapterError(
		"UPSTREAM_CREDENTIAL_ENCRYPTION_FAILED",
		"无法生成上游加密登录材料，请重新登录",
		model.IdentityStatusSyncError,
		http.StatusBadGateway,
	)
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func cookieMaterialNames(value string) []string {
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, part := range strings.Split(value, ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(part), "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func removeCookieMaterial(value string, names ...string) string {
	excluded := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			excluded[name] = struct{}{}
		}
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		name, _, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		if _, remove := excluded[name]; remove {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}
