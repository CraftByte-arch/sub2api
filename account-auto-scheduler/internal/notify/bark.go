package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	defaultBarkTimeout = 10 * time.Second
	maxBarkResponse    = 64 << 10
	barkGroup          = "sub2api-auto-scheduler"
)

type BarkClient struct {
	httpClient *http.Client
}

func NewBarkClient(client *http.Client) *BarkClient {
	if client == nil {
		client = &http.Client{Timeout: defaultBarkTimeout}
	}
	return &BarkClient{httpClient: client}
}

func (c *BarkClient) Send(ctx context.Context, settings model.NotificationSettings, secrets model.BarkNotificationSecrets, title, body string) error {
	endpoint, err := normalizeBarkEndpoint(settings.BarkEndpoint)
	if err != nil {
		return err
	}
	if strings.TrimSpace(secrets.DeviceKey) == "" {
		return errors.New("Bark 设备 Key 未配置")
	}
	if len([]byte(secrets.EncryptionKey)) != 16 {
		return errors.New("Bark 加密 Key 必须是 16 字节（AES-128）")
	}
	if strings.TrimSpace(title) == "" {
		title = "Sub2API 自动调度告警"
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("Bark 推送正文不能为空")
	}

	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"level": "active",
		"group": barkGroup,
	})
	if err != nil {
		return fmt.Errorf("编码 Bark 推送内容失败: %w", err)
	}
	iv, err := randomIV()
	if err != nil {
		return fmt.Errorf("生成 Bark 推送随机数失败: %w", err)
	}
	ciphertext, err := encryptAES128CBC([]byte(secrets.EncryptionKey), []byte(iv), payload)
	if err != nil {
		return fmt.Errorf("加密 Bark 推送内容失败: %w", err)
	}

	pushURL, err := barkDeviceURL(endpoint, secrets.DeviceKey)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("ciphertext", base64.StdEncoding.EncodeToString(ciphertext))
	form.Set("iv", iv)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, pushURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建 Bark 请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sub2api-account-auto-scheduler")
	if user := strings.TrimSpace(settings.BarkBasicAuthUser); user != "" {
		request.SetBasicAuth(user, secrets.BasicAuthPassword)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Bark 请求失败: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxBarkResponse))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Bark 返回 HTTP %d: %s", response.StatusCode, sanitizeResponse(string(responseBody)))
	}
	return nil
}

func normalizeBarkEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("Bark 地址必须是绝对 HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("Bark 地址不能包含凭证、查询参数或片段")
	}
	if parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) {
		return "", errors.New("Bark 地址必须使用 HTTPS；仅本机回环测试允许 HTTP")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(host))
	return parsed != nil && parsed.IsLoopback()
}

func barkDeviceURL(endpoint, deviceKey string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("Bark 地址无效")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + url.PathEscape(strings.TrimSpace(deviceKey))
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func randomIV() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", randomBytes), nil
}

func encryptAES128CBC(key, iv, plaintext []byte) ([]byte, error) {
	if len(key) != 16 || len(iv) != aes.BlockSize {
		return nil, errors.New("AES-128-CBC Key 或 IV 长度无效")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, len(plaintext)+padding)
	copy(padded, plaintext)
	for index := len(plaintext); index < len(padded); index++ {
		padded[index] = byte(padding)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return ciphertext, nil
}

func sanitizeResponse(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		value = value[:500]
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			return r
		}
		return ' '
	}, value)
}
