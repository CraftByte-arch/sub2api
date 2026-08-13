package upstream

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const credentialEnvelopeVersion = 1

var ErrCredentialsDisabled = errors.New("上游凭证加密未配置")

type AuthMaterial struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Cookie       string `json:"cookie,omitempty"`
	UserID       string `json:"user_id,omitempty"`
}

// EncryptDirectProbe keeps account probe credentials in a separate AEAD domain
// from upstream console login sessions, even though both use the deployment key.
func (b *CredentialBox) EncryptDirectProbe(accountID int64, snapshot model.DirectProbeSnapshot) (model.CredentialEnvelope, error) {
	if !b.Enabled() {
		return model.CredentialEnvelope{}, ErrCredentialsDisabled
	}
	if accountID <= 0 || snapshot.AccountID != accountID {
		return model.CredentialEnvelope{}, errors.New("直连探测账号无效")
	}
	if strings.TrimSpace(snapshot.APIKey) == "" || strings.TrimSpace(snapshot.BaseURL) == "" || !model.IsDirectProbePlatformSupported(snapshot.Platform) {
		return model.CredentialEnvelope{}, errors.New("直连探测凭证不完整或平台不受支持")
	}
	snapshot.Version = model.DirectProbeSnapshotVersion
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return model.CredentialEnvelope{}, fmt.Errorf("编码直连探测凭证失败: %w", err)
	}
	if len(raw) > model.MaxDirectProbeSnapshotSize {
		return model.CredentialEnvelope{}, errors.New("直连探测凭证过大")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return model.CredentialEnvelope{}, fmt.Errorf("生成直连探测随机数失败: %w", err)
	}
	ciphertext := b.aead.Seal(nil, nonce, raw, directProbeAAD(accountID))
	return model.CredentialEnvelope{
		Version:    credentialEnvelopeVersion,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	}, nil
}

func (b *CredentialBox) DecryptDirectProbe(accountID int64, envelope model.CredentialEnvelope) (model.DirectProbeSnapshot, error) {
	if !b.Enabled() {
		return model.DirectProbeSnapshot{}, ErrCredentialsDisabled
	}
	if accountID <= 0 || envelope.Version != credentialEnvelopeVersion {
		return model.DirectProbeSnapshot{}, errors.New("直连探测凭证版本无效")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != b.aead.NonceSize() {
		return model.DirectProbeSnapshot{}, errors.New("直连探测凭证随机数无效")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > model.MaxDirectProbeSnapshotSize+1024 {
		return model.DirectProbeSnapshot{}, errors.New("直连探测凭证密文无效")
	}
	raw, err := b.aead.Open(nil, nonce, ciphertext, directProbeAAD(accountID))
	if err != nil {
		return model.DirectProbeSnapshot{}, errors.New("无法解密直连探测凭证，请重新授权")
	}
	var snapshot model.DirectProbeSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Version != model.DirectProbeSnapshotVersion || snapshot.AccountID != accountID || strings.TrimSpace(snapshot.APIKey) == "" || strings.TrimSpace(snapshot.BaseURL) == "" || !model.IsDirectProbePlatformSupported(snapshot.Platform) {
		return model.DirectProbeSnapshot{}, errors.New("直连探测凭证内容无效")
	}
	return snapshot, nil
}

func (m AuthMaterial) Empty() bool {
	return strings.TrimSpace(m.AccessToken) == "" && strings.TrimSpace(m.RefreshToken) == "" && strings.TrimSpace(m.Cookie) == ""
}

type CredentialBox struct {
	aead           cipher.AEAD
	fingerprintKey []byte
}

func NewCredentialBox(encodedKey string) (*CredentialBox, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return &CredentialBox{}, nil
	}
	key, err := decodeCredentialKey(encodedKey)
	if err != nil {
		return &CredentialBox{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return &CredentialBox{}, fmt.Errorf("初始化上游凭证加密失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return &CredentialBox{}, fmt.Errorf("初始化上游凭证加密失败: %w", err)
	}
	deriver := hmac.New(sha256.New, key)
	_, _ = deriver.Write([]byte("sub2api-auto-scheduler/upstream-fingerprint/v1"))
	return &CredentialBox{aead: aead, fingerprintKey: deriver.Sum(nil)}, nil
}

func (b *CredentialBox) Enabled() bool {
	return b != nil && b.aead != nil && len(b.fingerprintKey) > 0
}

func (b *CredentialBox) Encrypt(upstreamID, identityID string, material AuthMaterial) (model.CredentialEnvelope, error) {
	if !b.Enabled() {
		return model.CredentialEnvelope{}, ErrCredentialsDisabled
	}
	if material.Empty() {
		return model.CredentialEnvelope{}, errors.New("没有可保存的上游会话凭证")
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return model.CredentialEnvelope{}, fmt.Errorf("编码上游会话凭证失败: %w", err)
	}
	if len(raw) > model.MaxUpstreamCredentialSize {
		return model.CredentialEnvelope{}, errors.New("上游会话凭证过大")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return model.CredentialEnvelope{}, fmt.Errorf("生成上游凭证随机数失败: %w", err)
	}
	ciphertext := b.aead.Seal(nil, nonce, raw, credentialAAD(upstreamID, identityID))
	return model.CredentialEnvelope{
		Version:    credentialEnvelopeVersion,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	}, nil
}

func (b *CredentialBox) Decrypt(upstreamID, identityID string, envelope model.CredentialEnvelope) (AuthMaterial, error) {
	if !b.Enabled() {
		return AuthMaterial{}, ErrCredentialsDisabled
	}
	if envelope.Version != credentialEnvelopeVersion {
		return AuthMaterial{}, errors.New("不支持的上游凭证版本")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != b.aead.NonceSize() {
		return AuthMaterial{}, errors.New("上游凭证随机数无效")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > model.MaxUpstreamCredentialSize+1024 {
		return AuthMaterial{}, errors.New("上游凭证密文无效")
	}
	raw, err := b.aead.Open(nil, nonce, ciphertext, credentialAAD(upstreamID, identityID))
	if err != nil {
		return AuthMaterial{}, errors.New("无法解密上游凭证，请重新连接")
	}
	var material AuthMaterial
	if err := json.Unmarshal(raw, &material); err != nil || material.Empty() {
		return AuthMaterial{}, errors.New("上游凭证内容无效")
	}
	return material, nil
}

func (b *CredentialBox) Fingerprint(secret string) (string, error) {
	if !b.Enabled() {
		return "", ErrCredentialsDisabled
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", errors.New("不能计算空凭证指纹")
	}
	digest := hmac.New(sha256.New, b.fingerprintKey)
	_, _ = digest.Write([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(digest.Sum(nil)), nil
}

func FingerprintsEqual(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	return hmac.Equal([]byte(first), []byte(second))
}

func decodeCredentialKey(value string) ([]byte, error) {
	if len(value) == 64 {
		if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	return nil, errors.New("AUTO_SCHEDULER_CREDENTIAL_KEY 必须是 base64 或 hex 编码的 32 字节密钥")
}

func credentialAAD(upstreamID, identityID string) []byte {
	return []byte("upstream-credential/v1\x00" + strings.TrimSpace(upstreamID) + "\x00" + strings.TrimSpace(identityID))
}

func directProbeAAD(accountID int64) []byte {
	return []byte("direct-probe-credential/v1\x00" + strconv.FormatInt(accountID, 10))
}
