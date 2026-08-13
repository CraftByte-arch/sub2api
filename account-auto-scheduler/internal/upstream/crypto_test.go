package upstream

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func testCredentialKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestCredentialBoxRoundTripAndAssociatedData(t *testing.T) {
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	material := AuthMaterial{AccessToken: "access-secret", RefreshToken: "refresh-secret", Cookie: "sid=cookie-secret", UserID: "7"}
	envelope, err := box.Encrypt("upstream-a", "identity-a", material)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(envelope.Ciphertext, "secret") || envelope.Nonce == "" {
		t.Fatalf("credential envelope was not encrypted: %#v", envelope)
	}
	got, err := box.Decrypt("upstream-a", "identity-a", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if got != material {
		t.Fatalf("decrypted material = %#v, want %#v", got, material)
	}
	if _, err := box.Decrypt("upstream-b", "identity-a", envelope); err == nil {
		t.Fatal("decrypt with different associated data unexpectedly succeeded")
	}
}

func TestCredentialBoxRejectsTampering(t *testing.T) {
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := box.Encrypt("upstream", "identity", AuthMaterial{AccessToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] ^= 0xff
	envelope.Ciphertext = base64.RawStdEncoding.EncodeToString(ciphertext)
	if _, err := box.Decrypt("upstream", "identity", envelope); err == nil {
		t.Fatal("tampered ciphertext unexpectedly decrypted")
	}
}

func TestCredentialBoxDirectProbeUsesSeparateAADAndRejectsTampering(t *testing.T) {
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.DirectProbeSnapshot{
		Version:   model.DirectProbeSnapshotVersion,
		AccountID: 9,
		Platform:  "openai",
		BaseURL:   "https://relay.example/v1",
		APIKey:    "direct-api-secret",
		HeaderOverrides: map[string]string{
			"X-Relay-Mode": "enabled",
		},
		Proxy: &model.DirectProbeProxy{
			Protocol: "http",
			Host:     "proxy.example",
			Port:     8080,
			Username: "proxy-user",
			Password: "proxy-password",
			Status:   "active",
		},
	}
	snapshot.RoutingFingerprint = model.DirectProbeRoutingFingerprint(snapshot)

	envelope, err := box.EncryptDirectProbe(snapshot.AccountID, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{snapshot.APIKey, snapshot.Proxy.Password, snapshot.BaseURL} {
		if strings.Contains(envelope.Ciphertext, secret) {
			t.Fatalf("direct probe ciphertext contains plaintext %q: %#v", secret, envelope)
		}
	}
	got, err := box.DecryptDirectProbe(snapshot.AccountID, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != snapshot.APIKey || got.Proxy == nil || got.Proxy.Password != snapshot.Proxy.Password || got.HeaderOverrides["X-Relay-Mode"] != "enabled" {
		t.Fatalf("unexpected decrypted direct snapshot: %#v", got)
	}
	if _, err := box.DecryptDirectProbe(snapshot.AccountID+1, envelope); err == nil {
		t.Fatal("direct snapshot decrypted for another account")
	}
	if _, err := box.Decrypt("upstream", "identity", envelope); err == nil {
		t.Fatal("direct snapshot decrypted in upstream-login AEAD domain")
	}
	loginEnvelope, err := box.Encrypt("upstream", "identity", AuthMaterial{AccessToken: "login-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.DecryptDirectProbe(snapshot.AccountID, loginEnvelope); err == nil {
		t.Fatal("upstream-login envelope decrypted in direct-probe AEAD domain")
	}

	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] ^= 0xff
	envelope.Ciphertext = base64.RawStdEncoding.EncodeToString(ciphertext)
	if _, err := box.DecryptDirectProbe(snapshot.AccountID, envelope); err == nil {
		t.Fatal("tampered direct snapshot unexpectedly decrypted")
	}
}

func TestCredentialBoxDisabled(t *testing.T) {
	box, err := NewCredentialBox("")
	if err != nil {
		t.Fatal(err)
	}
	if box.Enabled() {
		t.Fatal("empty credential key unexpectedly enabled encryption")
	}
	if _, err := box.Encrypt("up", "id", AuthMaterial{AccessToken: "secret"}); !errors.Is(err, ErrCredentialsDisabled) {
		t.Fatalf("Encrypt error = %v, want ErrCredentialsDisabled", err)
	}
	if _, err := box.Fingerprint("secret"); !errors.Is(err, ErrCredentialsDisabled) {
		t.Fatalf("Fingerprint error = %v, want ErrCredentialsDisabled", err)
	}
}

func TestCredentialBoxRejectsInvalidKey(t *testing.T) {
	if _, err := NewCredentialBox("short"); err == nil {
		t.Fatal("invalid key unexpectedly succeeded")
	}
}

func TestFingerprintsAreStableAndConstantTimeComparable(t *testing.T) {
	box, err := NewCredentialBox(testCredentialKey())
	if err != nil {
		t.Fatal(err)
	}
	first, err := box.Fingerprint("sk-same")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := box.Fingerprint("sk-same")
	different, _ := box.Fingerprint("sk-different")
	if !FingerprintsEqual(first, second) || FingerprintsEqual(first, different) || strings.Contains(first, "sk-") {
		t.Fatalf("unexpected fingerprints: %q %q %q", first, second, different)
	}
}
