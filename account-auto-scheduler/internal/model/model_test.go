package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestNormalizeUpstreamBaseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "HTTPS://Example.COM:443/v1/", want: "https://example.com"},
		{raw: "http://Example.COM:80/panel/api/v1", want: "http://example.com/panel"},
		{raw: "https://example.com/sub2api/v1", want: "https://example.com/sub2api"},
		{raw: "https://example.com:8443/root/", want: "https://example.com:8443/root"},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			got, err := NormalizeUpstreamBaseURL(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("NormalizeUpstreamBaseURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeUpstreamBaseURLRejectsUnsafeParts(t *testing.T) {
	for _, raw := range []string{
		"file:///tmp/upstream",
		"https://user:password@example.com",
		"https://example.com?token=secret",
		"https://example.com/#secret",
	} {
		if _, err := NormalizeUpstreamBaseURL(raw); err == nil {
			t.Fatalf("NormalizeUpstreamBaseURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestManagedUpstreamEffectiveManagementURLFallsBackToAPIAddress(t *testing.T) {
	upstream := ManagedUpstream{BaseURL: "https://api.example.com"}
	if got := upstream.EffectiveManagementURL(); got != upstream.BaseURL {
		t.Fatalf("effective management URL = %q, want API address %q", got, upstream.BaseURL)
	}

	upstream.ManagementURL = "https://panel.example.com"
	if got := upstream.EffectiveManagementURL(); got != upstream.ManagementURL {
		t.Fatalf("effective management URL = %q, want configured site %q", got, upstream.ManagementURL)
	}
}

func TestStableUpstreamIDIsDeterministicAndOpaque(t *testing.T) {
	first := StableUpstreamID("https://example.com")
	second := StableUpstreamID("https://example.com")
	if first != second || !strings.HasPrefix(first, "up_") || strings.Contains(first, "example") {
		t.Fatalf("unexpected stable ID: %q / %q", first, second)
	}
}

func TestNewUpstreamRechargeRateNormalizesBothInputModes(t *testing.T) {
	now := time.Date(2026, 8, 11, 1, 2, 3, 0, time.FixedZone("UTC+8", 8*60*60))
	usdPerCNY, err := NewUpstreamRechargeRate(RechargeRateUSDPerCNY, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(usdPerCNY.CNYPerUSD-0.2) > 1e-12 || usdPerCNY.InputValue != 5 || usdPerCNY.UpdatedAt.Location() != time.UTC {
		t.Fatalf("unexpected USD-per-CNY normalization: %#v", usdPerCNY)
	}
	cnyPerUSD, err := NewUpstreamRechargeRate(RechargeRateCNYPerUSD, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if cnyPerUSD.CNYPerUSD != 5 || cnyPerUSD.InputValue != 5 {
		t.Fatalf("unexpected CNY-per-USD normalization: %#v", cnyPerUSD)
	}
	for _, input := range []struct {
		mode  RechargeRateInputMode
		value float64
	}{
		{mode: RechargeRateUSDPerCNY, value: 0},
		{mode: RechargeRateCNYPerUSD, value: math.Inf(1)},
		{mode: "unknown", value: 1},
	} {
		if _, err := NewUpstreamRechargeRate(input.mode, input.value, now); err == nil {
			t.Fatalf("invalid recharge rate unexpectedly succeeded: %#v", input)
		}
	}
}

func TestNormalizeRemoteGroupPlatformKeepsOnlyExplicitSupportedPlatforms(t *testing.T) {
	tests := map[string]string{
		" OpenAI ":  "openai",
		"ANTHROPIC": "anthropic",
		"gemini":    "gemini",
		"Grok":      "grok",
		"composite": "",
		"custom":    "",
		"":          "",
	}
	for input, want := range tests {
		if got := NormalizeRemoteGroupPlatform(input); got != want {
			t.Fatalf("NormalizeRemoteGroupPlatform(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPolicyNormalizeUsesChannelMonitorPrompt(t *testing.T) {
	policy := DefaultPolicy()
	policy.Prompt = "  "
	normalized, err := policy.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Prompt != DefaultPrompt {
		t.Fatalf("prompt = %q, want default channel monitor prompt", normalized.Prompt)
	}
}

func TestPolicyNormalizePreservesCustomPromptVerbatim(t *testing.T) {
	policy := DefaultPolicy()
	policy.Prompt = "  keep this custom prompt exactly  \n"

	normalized, err := policy.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Prompt != policy.Prompt {
		t.Fatalf("custom prompt = %q, want %q", normalized.Prompt, policy.Prompt)
	}
}

func TestPolicyNormalizeRejectsUnsafeRanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Policy)
	}{
		{name: "short interval", change: func(p *Policy) { p.IntervalSeconds = 14 }},
		{name: "long latency", change: func(p *Policy) { p.LatencyLimitMS = 300001 }},
		{name: "zero failure threshold", change: func(p *Policy) { p.FailureThreshold = 0 }},
		{name: "large recovery threshold", change: func(p *Policy) { p.RecoveryThreshold = 21 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := DefaultPolicy()
			test.change(&policy)
			if _, err := policy.Normalize(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpstreamAccountProjectsDetectedRateWithoutForwardingExtra(t *testing.T) {
	var account UpstreamAccount
	payload := `{
		"id": 7,
		"name": "metered-key",
		"type": "apikey",
		"quota_limit": 100,
		"quota_used": 12.5,
		"quota_daily_limit": 10,
		"quota_daily_used": 2.25,
		"credentials": {"base_url": "https://upstream.example/v1"},
		"extra": {
			"private_note": "must-not-leak",
			"mixed_scheduling": true,
			"upstream_billing_probe": {
				"status": "ok",
				"received_at": "2026-08-09T01:02:03Z",
				"last_attempt_at": "2026-08-09T01:02:02Z",
				"data": {
					"effective_rate_multiplier": 0.75,
					"observed_at": "2026-08-09T01:02:01Z",
					"ignored": "not-forwarded"
				}
			}
		}
	}`
	if err := json.Unmarshal([]byte(payload), &account); err != nil {
		t.Fatal(err)
	}
	if account.QuotaLimit == nil || *account.QuotaLimit != 100 || account.QuotaDailyLimit == nil || *account.QuotaDailyLimit != 10 {
		t.Fatalf("configured quotas were not decoded: %#v", account)
	}
	if !account.MixedScheduling {
		t.Fatal("mixed scheduling flag was not projected")
	}
	if account.BaseURL() != "https://upstream.example/v1" {
		t.Fatalf("base URL = %q", account.BaseURL())
	}
	if account.DetectedRate == nil || account.DetectedRate.EffectiveMultiplier == nil || *account.DetectedRate.EffectiveMultiplier != 0.75 {
		t.Fatalf("detected rate was not projected: %#v", account.DetectedRate)
	}
	if account.DetectedRate.ObservedAt != "2026-08-09T01:02:01Z" || account.DetectedRate.ReceivedAt == nil || account.DetectedRate.LastAttemptAt == nil {
		t.Fatalf("detected rate timestamps were not projected: %#v", account.DetectedRate)
	}

	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	if strings.Contains(serialized, "private_note") || strings.Contains(serialized, "ignored") || strings.Contains(serialized, `"extra"`) || strings.Contains(serialized, `"credentials"`) || strings.Contains(serialized, "upstream.example") {
		t.Fatalf("arbitrary extra data was forwarded: %s", serialized)
	}
	if !strings.Contains(serialized, `"detected_rate"`) || !strings.Contains(serialized, `"effective_multiplier":0.75`) {
		t.Fatalf("sanitized detected rate is missing: %s", serialized)
	}
	if !strings.Contains(serialized, `"mixed_scheduling":true`) {
		t.Fatalf("sanitized mixed scheduling flag is missing: %s", serialized)
	}
}

func TestUpstreamAccountKeepsDetectedRateStateWithoutInvalidMultiplier(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		wantStatus string
		wantRate   bool
	}{
		{name: "missing probe", payload: `{"id":1,"extra":{"private":"value"}}`},
		{name: "failed probe", payload: `{"id":1,"extra":{"upstream_billing_probe":{"status":"failed","last_error":"timeout"}}}`, wantStatus: "failed"},
		{name: "negative multiplier", payload: `{"id":1,"extra":{"upstream_billing_probe":{"status":"ok","data":{"effective_rate_multiplier":-2}}}}`, wantStatus: "ok"},
		{name: "zero multiplier", payload: `{"id":1,"extra":{"upstream_billing_probe":{"status":"ok","data":{"effective_rate_multiplier":0}}}}`, wantStatus: "ok", wantRate: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var account UpstreamAccount
			if err := json.Unmarshal([]byte(test.payload), &account); err != nil {
				t.Fatal(err)
			}
			if test.wantStatus == "" {
				if account.DetectedRate != nil {
					t.Fatalf("unexpected detected rate: %#v", account.DetectedRate)
				}
				return
			}
			if account.DetectedRate == nil || account.DetectedRate.Status != test.wantStatus {
				t.Fatalf("detected state = %#v, want %q", account.DetectedRate, test.wantStatus)
			}
			if (account.DetectedRate.EffectiveMultiplier != nil) != test.wantRate {
				t.Fatalf("effective multiplier = %#v, want present=%v", account.DetectedRate.EffectiveMultiplier, test.wantRate)
			}
		})
	}
}

func TestManagedAccountPublicViewRedactsDirectProbeCredentials(t *testing.T) {
	now := time.Now().UTC()
	managed := ManagedAccount{
		AccountID:   17,
		Name:        "direct account",
		Policy:      DefaultPolicy(),
		ProbeSource: ProbeSourceDirect,
		DirectProbe: &DirectProbeConfig{
			Credential: CredentialEnvelope{
				Version:    1,
				Nonce:      "encrypted-nonce",
				Ciphertext: "direct-api-secret-must-not-leak",
			},
			AuthorizationState: DirectProbeAuthorized,
			ImportedAt:         &now,
			RoutingFingerprint: "routing-fingerprint-must-not-leak",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	raw, err := json.Marshal(managed.PublicView())
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, forbidden := range []string{"direct-api-secret-must-not-leak", "encrypted-nonce", "routing-fingerprint-must-not-leak", "credential"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("public view leaked direct-probe secret material: %s", serialized)
		}
	}
	if !strings.Contains(serialized, `"source":"direct"`) || !strings.Contains(serialized, `"authorization_state":"authorized"`) {
		t.Fatalf("public view omitted direct-probe metadata: %s", serialized)
	}
}

func TestManagedAccountPublicViewIncludesBalanceFailureClassification(t *testing.T) {
	now := time.Now().UTC()
	managed := ManagedAccount{
		AccountID:       21,
		LastError:       "直连上游拒绝访问: 用户额度不足",
		LastFailureKind: CheckFailureBalanceInsufficient,
		History: []CheckResult{{
			ID:          "check-balance",
			Status:      CheckError,
			FailureKind: CheckFailureBalanceInsufficient,
			Message:     "直连上游拒绝访问: 用户额度不足",
			CheckedAt:   now,
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	view := managed.PublicView()
	if view.LastFailureKind != CheckFailureBalanceInsufficient || len(view.History) != 1 || view.History[0].FailureKind != CheckFailureBalanceInsufficient {
		t.Fatalf("public view omitted balance failure classification: %#v", view)
	}
}
