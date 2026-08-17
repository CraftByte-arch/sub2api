package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestEncryptAES128CBCMatchesBarkDocumentedExample(t *testing.T) {
	ciphertext, err := encryptAES128CBC(
		[]byte("1234567890123456"),
		[]byte("1234567890123456"),
		[]byte(`{"body": "test", "sound": "birdsong"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	got := base64.StdEncoding.EncodeToString(ciphertext)
	want := "+aPt5cwN9GbTLLSFri60l3h1X00u/9j1FENfWiTxhNHVLGU+XoJ15JJG5W/d/yf0"
	if got != want {
		t.Fatalf("ciphertext = %q, want documented Bark value %q", got, want)
	}
}

func TestBarkClientSendsEncryptedFormAndBasicAuth(t *testing.T) {
	const encryptionKey = "1234567890123456"
	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/device-key" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "bark-user" || password != "bark-password" {
			t.Fatalf("unexpected basic auth: ok=%v user=%q password=%q", ok, user, password)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		plaintext := decryptBarkTestPayload(t, encryptionKey, r.Form)
		if err := json.Unmarshal(plaintext, &received); err != nil {
			t.Fatalf("decode encrypted Bark payload: %v payload=%s", err, plaintext)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200,"message":"success"}`)
	}))
	defer server.Close()

	client := NewBarkClient(server.Client())
	err := client.Send(context.Background(), model.NotificationSettings{
		BarkEndpoint:      server.URL,
		BarkBasicAuthUser: "bark-user",
	}, model.BarkNotificationSecrets{
		DeviceKey:         "device-key",
		EncryptionKey:     encryptionKey,
		BasicAuthPassword: "bark-password",
	}, "余额不足告警", "分组-账号，可用余额低于 10")
	if err != nil {
		t.Fatal(err)
	}
	if received["title"] != "余额不足告警" || received["body"] != "分组-账号，可用余额低于 10" || received["group"] != barkGroup || received["level"] != "active" {
		t.Fatalf("unexpected Bark payload: %#v", received)
	}
}

func TestBarkClientRejectsSecretBearingEndpointAndDoesNotEchoSecrets(t *testing.T) {
	client := NewBarkClient(nil)
	secret := "device-secret"
	err := client.Send(context.Background(), model.NotificationSettings{BarkEndpoint: "https://user:pass@example.com"}, model.BarkNotificationSecrets{
		DeviceKey: secret, EncryptionKey: "1234567890123456",
	}, "title", "body")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func decryptBarkTestPayload(t *testing.T, key string, form url.Values) []byte {
	t.Helper()
	iv := []byte(form.Get("iv"))
	if len(iv) != aes.BlockSize {
		t.Fatalf("invalid Bark IV: %q", iv)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(form.Get("ciphertext"))
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	padding := int(plaintext[len(plaintext)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plaintext) {
		t.Fatalf("invalid PKCS#7 padding: %d", padding)
	}
	return plaintext[:len(plaintext)-padding]
}
