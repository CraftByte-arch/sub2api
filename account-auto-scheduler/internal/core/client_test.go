package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func TestNormalizeAPIRoot(t *testing.T) {
	tests := map[string]string{
		"https://example.com":                "https://example.com/api/v1",
		"https://example.com/":               "https://example.com/api/v1",
		"https://example.com/sub2api":        "https://example.com/sub2api/api/v1",
		"https://example.com/sub2api/api/v1": "https://example.com/sub2api/api/v1",
	}
	for input, expected := range tests {
		actual, err := normalizeAPIRoot(input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if actual != expected {
			t.Fatalf("normalize %q: got %q, want %q", input, actual, expected)
		}
	}
}

func TestTestAccountParsesSuccessfulSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/accounts/7/test" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatal("missing admin API key")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model_id"] != "gpt-test" || payload["prompt"] != "hello" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"test_start\"}\n\n")
		_, _ = fmt.Fprint(w, "data:{\"type\":\"content\",\"text\":\"4\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"test_complete\",\"success\":true,\"model\":\"gpt-test\",\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2}}\n\n")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	outcome, err := client.TestAccount(context.Background(), 7, "gpt-test", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Success || outcome.ResponseText != "4" || outcome.ErrorMessage != "" || outcome.Usage == nil || outcome.Usage.InputTokens != 8 || outcome.Usage.OutputTokens != 2 || outcome.Usage.Model != "gpt-test" {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
}

func TestGetModelPricingUsesExistingAdministratorEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/channels/model-pricing" || r.URL.Query().Get("model") != "gpt-test" || r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatalf("unexpected pricing request: %s?%s headers=%#v", r.URL.Path, r.URL.RawQuery, r.Header)
		}
		writeEnvelope(t, w, map[string]any{
			"found": true, "input_price": 0.001, "output_price": 0.002,
			"cache_write_price": 0.00125, "cache_read_price": 0.0001,
		})
	}))
	defer server.Close()

	pricing, err := newTestClient(t, server.URL).GetModelPricing(context.Background(), "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	if !pricing.Found || pricing.InputPrice == nil || *pricing.InputPrice != 0.001 || pricing.OutputPrice == nil || *pricing.OutputPrice != 0.002 || pricing.CacheReadPrice == nil || *pricing.CacheReadPrice != 0.0001 {
		t.Fatalf("unexpected pricing: %#v", pricing)
	}
}

func TestTestAccountRequiresCompleteSuccessEvent(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		wantError string
	}{
		{name: "error event", stream: "data: {\"type\":\"error\",\"error\":\"upstream failed\"}\n\n", wantError: "upstream failed"},
		{name: "truncated stream", stream: "data: {\"type\":\"content\",\"text\":\"partial\"}\n\n", wantError: "stream ended before test_complete"},
		{name: "false completion", stream: "data: {\"type\":\"test_complete\",\"success\":false}\n\n", wantError: "account test did not report success"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, test.stream)
			}))
			defer server.Close()

			outcome, err := newTestClient(t, server.URL).TestAccount(context.Background(), 1, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Success || outcome.ErrorMessage != test.wantError {
				t.Fatalf("unexpected outcome: %#v", outcome)
			}
		})
	}
}

func TestListAPIKeyAccountsPaginatesAndFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "apikey" || r.URL.Query().Get("lite") != "true" {
			t.Fatalf("missing filters: %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("page")
		pageNumber := 1
		items := []map[string]any{{"id": 1, "name": "first", "type": "apikey", "platform": "openai", "status": "active", "schedulable": true}}
		if page == "2" {
			pageNumber = 2
			items = []map[string]any{{"id": 2, "name": "second", "type": "apikey", "platform": "anthropic", "status": "active", "schedulable": false}}
		}
		writeEnvelope(t, w, map[string]any{"items": items, "total": 2, "page": pageNumber, "page_size": 100, "pages": 2})
	}))
	defer server.Close()

	accounts, err := newTestClient(t, server.URL).ListAPIKeyAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].ID != 1 || accounts[1].ID != 2 || accounts[1].Schedulable {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}
}

func TestListAccountsPaginatesAndKeepsConsoleFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "" || r.URL.Query().Get("lite") != "" {
			t.Fatalf("unexpected filters: %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("page")
		pageNumber := 1
		items := []map[string]any{{
			"id": 1, "name": "oauth", "type": "oauth", "platform": "openai", "status": "active",
			"schedulable": true, "group_ids": []int64{7}, "quota_used": 1.25,
		}}
		if page == "2" {
			pageNumber = 2
			items = []map[string]any{{
				"id": 2, "name": "key", "type": "apikey", "platform": "openai", "status": "error",
				"schedulable": false, "error_message": "upstream rejected", "group_ids": []int64{7, 8},
				"quota_limit": 100.0, "quota_used": 12.5, "quota_daily_limit": 10.0, "quota_daily_used": 2.5,
				"extra": map[string]any{
					"credential_hint":       "must-not-leak",
					"final_cost_multiplier": 0.16,
					"upstream_billing_probe": map[string]any{
						"status": "ok", "received_at": "2026-08-09T01:02:03Z",
						"data": map[string]any{"effective_rate_multiplier": 1.25, "observed_at": "2026-08-09T01:02:01Z"},
					},
				},
			}}
		}
		writeEnvelope(t, w, map[string]any{"items": items, "total": 2, "page": pageNumber, "page_size": 100, "pages": 2})
	}))
	defer server.Close()

	accounts, err := newTestClient(t, server.URL).ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || !accounts[0].IsOAuthLike() || !accounts[1].IsAPIKey() {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}
	if !reflect.DeepEqual(accounts[1].GroupIDs, []int64{7, 8}) || accounts[1].ErrorMessage != "upstream rejected" {
		t.Fatalf("console fields were not preserved: %#v", accounts[1])
	}
	if accounts[1].QuotaLimit == nil || *accounts[1].QuotaLimit != 100 || accounts[1].QuotaDailyUsed == nil || *accounts[1].QuotaDailyUsed != 2.5 {
		t.Fatalf("configured quota fields were not preserved: %#v", accounts[1])
	}
	if accounts[1].DetectedRate == nil || accounts[1].DetectedRate.EffectiveMultiplier == nil || *accounts[1].DetectedRate.EffectiveMultiplier != 1.25 {
		t.Fatalf("detected multiplier was not projected: %#v", accounts[1].DetectedRate)
	}
	if multiplier, ok := accounts[1].FinalCostMultiplier(); !ok || multiplier == nil || *multiplier != 0.16 {
		t.Fatalf("final cost multiplier was not projected: %#v", accounts[1].Extra)
	}
	encoded, err := json.Marshal(accounts[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "credential_hint") || strings.Contains(string(encoded), `"extra"`) {
		t.Fatalf("account extra leaked through client model: %s", encoded)
	}
}

func TestSetFinalCostMultiplierUpdatesOnlySchedulingExtra(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/accounts/bulk-update" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatal("missing admin API key")
		}
		var payload struct {
			AccountIDs []int64        `json:"account_ids"`
			Extra      map[string]any `json:"extra"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload.AccountIDs, []int64{7}) || payload.Extra[model.FinalCostMultiplierExtraKey] != 0.16 {
			t.Fatalf("unexpected final cost payload: %#v", payload)
		}
		writeEnvelope(t, w, map[string]any{"success": 1, "failed": 0, "success_ids": []int64{7}})
	}))
	defer server.Close()

	multiplier := 0.16
	_, err := newTestClient(t, server.URL).SetFinalCostMultiplier(context.Background(), 7, &multiplier)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetAccountBalanceQuotaUsesExistingBulkExtraMerge(t *testing.T) {
	observedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/accounts/bulk-update" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			AccountIDs []int64        `json:"account_ids"`
			Extra      map[string]any `json:"extra"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(payload.AccountIDs, []int64{7}) {
			t.Fatalf("unexpected account ids: %#v", payload.AccountIDs)
		}
		if payload.Extra["quota_limit"] != 200.0 || payload.Extra["quota_used"] != 0.0 ||
			payload.Extra[model.UpstreamBalanceQuotaManagedExtraKey] != true ||
			payload.Extra[model.UpstreamBalanceQuotaRemainingExtraKey] != 200.0 ||
			payload.Extra[model.UpstreamBalanceQuotaObservedAtExtraKey] != observedAt.Format(time.RFC3339Nano) {
			t.Fatalf("unexpected quota payload: %#v", payload.Extra)
		}
		if _, exists := payload.Extra["rate_multiplier"]; exists {
			t.Fatalf("quota refresh sent billing multiplier: %#v", payload.Extra)
		}
		writeEnvelope(t, w, map[string]any{"success": 1, "failed": 0, "success_ids": []int64{7}})
	}))
	defer server.Close()

	remaining := 200.0
	zero := 0.0
	err := newTestClient(t, server.URL).SetAccountBalanceQuota(context.Background(), 7, model.AccountBalanceQuotaUpdate{
		QuotaLimit: 200,
		QuotaUsed:  &zero,
		Managed:    true,
		Remaining:  &remaining,
		ObservedAt: &observedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetAccountBalanceQuotaSendsImmediateExhaustionSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Extra map[string]any `json:"extra"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Extra["quota_limit"] != 1e-9 || payload.Extra["quota_used"] != 1e-9 ||
			payload.Extra[model.UpstreamBalanceQuotaExhaustedExtraKey] != true {
			t.Fatalf("unexpected exhaustion payload: %#v", payload.Extra)
		}
		writeEnvelope(t, w, map[string]any{"success": 1})
	}))
	defer server.Close()

	sentinel := 1e-9
	remaining := 0.0
	err := newTestClient(t, server.URL).SetAccountBalanceQuota(context.Background(), 7, model.AccountBalanceQuotaUpdate{
		QuotaLimit: sentinel,
		QuotaUsed:  &sentinel,
		Managed:    true,
		Remaining:  &remaining,
		Exhausted:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsoleDataMethodsNormalizeEnvelopes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/admin/groups/all":
			if r.URL.Query().Get("include_inactive") != "true" {
				t.Fatalf("missing include_inactive: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, []map[string]any{{
				"id": 7, "name": "Primary", "platform": "openai", "status": "active",
				"sort_order": 3, "account_count": 9, "active_account_count": 6,
			}})
		case "POST /api/v1/admin/accounts/today-stats/batch":
			var payload struct {
				AccountIDs []int64 `json:"account_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(payload.AccountIDs, []int64{1, 2}) {
				t.Fatalf("unexpected normalized IDs: %#v", payload.AccountIDs)
			}
			writeEnvelope(t, w, map[string]any{"stats": map[string]any{
				"1": map[string]any{"requests": 4, "tokens": 1200, "cost": 0.75},
			}})
		case "GET /api/v1/admin/accounts/1/stats":
			if r.URL.Query().Get("days") != "1" {
				t.Fatalf("unexpected account stats range: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{"models": []map[string]any{
				{"input_tokens": 300, "cache_creation_tokens": 100, "cache_read_tokens": 600},
			}})
		case "GET /api/v1/admin/accounts/2/stats":
			writeEnvelope(t, w, map[string]any{"models": []map[string]any{}})
		case "GET /api/v1/admin/accounts/1/usage":
			if r.URL.Query().Get("source") != "passive" {
				t.Fatalf("unexpected usage source: %s", r.URL.RawQuery)
			}
			writeEnvelope(t, w, map[string]any{
				"source": "passive", "updated_at": "2026-08-09T01:00:00Z",
				"five_hour": map[string]any{"utilization": 42.5, "remaining_seconds": 300},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	groups, err := client.ListGroups(context.Background())
	if err != nil || len(groups) != 1 || groups[0].ActiveAccountCount != 6 {
		t.Fatalf("unexpected groups: %#v err=%v", groups, err)
	}
	stats, err := client.GetTodayStatsBatch(context.Background(), []int64{2, 1, 2, 0})
	if err != nil || stats["1"].Tokens != 1200 || stats["1"].Cache == nil || stats["1"].Cache.HitRate != 60 {
		t.Fatalf("unexpected stats: %#v err=%v", stats, err)
	}
	if stats["2"].Cache == nil || stats["2"].Cache.PromptTokens != 0 {
		t.Fatalf("idle account did not receive zero cache projection: %#v", stats["2"])
	}
	usage, err := client.GetPassiveUsage(context.Background(), 1)
	if err != nil || usage.FiveHour == nil || usage.FiveHour.Utilization != 42.5 {
		t.Fatalf("unexpected usage: %#v err=%v", usage, err)
	}
}

func TestSetAccountGroupPreservesOtherMemberships(t *testing.T) {
	var received []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeEnvelope(t, w, map[string]any{
				"id": 9, "name": "account", "type": "oauth", "platform": "openai", "status": "active",
				"schedulable": true, "group_ids": []int64{5, 1},
			})
		case http.MethodPut:
			var payload struct {
				GroupIDs []int64 `json:"group_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			received = payload.GroupIDs
			writeEnvelope(t, w, map[string]any{
				"id": 9, "name": "account", "type": "oauth", "platform": "openai", "status": "active",
				"schedulable": true, "group_ids": payload.GroupIDs,
			})
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()

	updated, err := newTestClient(t, server.URL).SetAccountGroup(context.Background(), 9, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(received, []int64{1, 3, 5}) || !reflect.DeepEqual(updated.GroupIDs, received) {
		t.Fatalf("memberships were not preserved: sent=%v updated=%v", received, updated.GroupIDs)
	}
}

func TestSetAccountGroupReturnsTypedMixedChannelConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeEnvelope(t, w, map[string]any{
				"id": 4, "name": "account", "type": "oauth", "platform": "anthropic", "status": "active",
				"schedulable": true, "group_ids": []int64{},
			})
			return
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "mixed_channel_warning: incompatible account"})
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).SetAccountGroup(context.Background(), 4, 8, true)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		t.Fatalf("expected typed conflict, got %T %v", err, err)
	}
}

func TestRegisterAdminMenuPreservesExistingItems(t *testing.T) {
	var updated []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/admin/settings":
			writeEnvelope(t, w, map[string]any{"custom_menu_items": []map[string]any{{"id": "existing", "label": "Existing", "sort_order": 4}}})
		case "PUT /api/v1/admin/settings":
			var payload map[string][]map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			updated = payload["custom_menu_items"]
			writeEnvelope(t, w, map[string]any{})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if err := newTestClient(t, server.URL).RegisterAdminMenu(context.Background(), "https://scheduler.example.com/panel"); err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 || updated[0]["id"] != "existing" {
		t.Fatalf("existing menu was not preserved: %#v", updated)
	}
	created := updated[1]
	if created["id"] != menuItemID || created["visibility"] != "admin" || created["url"] != "https://scheduler.example.com/panel/" {
		t.Fatalf("unexpected scheduler menu: %#v", created)
	}
}

func TestValidateAdminJWTForwardsBrowserIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-token" || r.Header.Get("X-Forwarded-For") != "203.0.113.8" || r.UserAgent() != "browser-agent" {
			t.Fatalf("unexpected identity headers: %#v", r.Header)
		}
		writeEnvelope(t, w, map[string]any{"id": 9, "email": "admin@example.com", "role": "admin"})
	}))
	defer server.Close()

	user, err := newTestClient(t, server.URL).ValidateAdminJWT(context.Background(), "jwt-token", ForwardedIdentity{ClientIP: "203.0.113.8", UserAgent: "browser-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(user, AdminUser{ID: 9, Email: "admin@example.com", Role: "admin"}) {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestExportAPIKeySecretsUsesOnlyForwardedAdminJWTPerAccount(t *testing.T) {
	requestedIDs := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/accounts/data" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer browser-jwt" || r.Header.Get("x-api-key") != "" {
			t.Fatalf("unexpected authorization headers: %#v", r.Header)
		}
		if r.Header.Get("X-Forwarded-For") != "203.0.113.8" || r.Header.Get("X-Real-IP") != "203.0.113.8" || r.UserAgent() != "browser-agent" {
			t.Fatalf("unexpected forwarded identity: %#v", r.Header)
		}
		ids := r.URL.Query().Get("ids")
		if strings.Contains(ids, ",") || r.URL.Query().Get("include_proxies") != "false" {
			t.Fatalf("export was not scoped to one account: %s", r.URL.RawQuery)
		}
		requestedIDs = append(requestedIDs, ids)
		writeEnvelope(t, w, map[string]any{"accounts": []map[string]any{{
			"type": "apikey", "credentials": map[string]any{"api_key": "sk-" + ids},
		}}})
	}))
	defer server.Close()

	secrets, err := newTestClient(t, server.URL).ExportAPIKeySecrets(
		context.Background(), []int64{9, 3, 9}, "browser-jwt",
		ForwardedIdentity{ClientIP: "203.0.113.8", UserAgent: "browser-agent"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requestedIDs, []string{"3", "9"}) || secrets[3] != "sk-3" || secrets[9] != "sk-9" {
		t.Fatalf("unexpected export result: ids=%#v secrets=%#v", requestedIDs, secrets)
	}
}

func TestExportAPIKeySecretsPreservesStepUpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer browser-jwt" || r.Header.Get("x-api-key") != "" {
			t.Fatalf("unexpected authorization headers: %#v", r.Header)
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "STEP_UP_REQUIRED", "message": "请先完成二次验证"})
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).ExportAPIKeySecrets(context.Background(), []int64{3}, "browser-jwt", ForwardedIdentity{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden || httpErr.Code != "STEP_UP_REQUIRED" || httpErr.Message != "请先完成二次验证" {
		t.Fatalf("unexpected step-up error: %T %#v", err, err)
	}
}

func TestExportDirectProbeSnapshotForwardsOnlyBrowserAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/accounts/data" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("ids") != "7" || r.URL.Query().Get("include_proxies") != "true" {
			t.Fatalf("unexpected export scope: %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer browser-jwt" || r.Header.Get("x-api-key") != "" {
			t.Fatalf("export did not use only browser JWT: %#v", r.Header)
		}
		if r.Header.Get("X-Forwarded-For") != "203.0.113.8" || r.Header.Get("X-Real-IP") != "203.0.113.8" || r.UserAgent() != "browser-agent" {
			t.Fatalf("export did not forward browser identity: %#v", r.Header)
		}
		writeEnvelope(t, w, map[string]any{
			"accounts": []map[string]any{{
				"platform":  "openai",
				"type":      "apikey",
				"proxy_key": "http|proxy.example|8080|proxy-user|proxy-password",
				"credentials": map[string]any{
					"api_key":                 "direct-api-secret",
					"base_url":                "https://relay.example/v1",
					"model_mapping":           map[string]any{"gpt-default": "gpt-upstream"},
					"header_override_enabled": true,
					"header_overrides": map[string]any{
						"X-Relay-Mode":  "enabled",
						"Authorization": "must-not-override",
					},
				},
				"extra": map[string]any{
					"openai_responses_mode":      "force_responses",
					"openai_responses_supported": true,
				},
			}},
			"proxies": []map[string]any{{
				"proxy_key": "http|proxy.example|8080|proxy-user|proxy-password",
				"protocol":  "http",
				"host":      "proxy.example",
				"port":      8080,
				"username":  "proxy-user",
				"password":  "proxy-password",
				"status":    "active",
			}},
		})
	}))
	defer server.Close()

	exported, err := newTestClient(t, server.URL).ExportDirectProbeSnapshot(
		context.Background(), 7, "browser-jwt", ForwardedIdentity{ClientIP: "203.0.113.8", UserAgent: "browser-agent"},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := exported.Snapshot
	if snapshot.AccountID != 7 || snapshot.Platform != "openai" || snapshot.APIKey != "direct-api-secret" || snapshot.BaseURL != "https://relay.example/v1" || snapshot.Proxy == nil || snapshot.Proxy.Password != "proxy-password" {
		t.Fatalf("unexpected direct snapshot: %#v", snapshot)
	}
	if snapshot.HeaderOverrides["X-Relay-Mode"] != "enabled" || snapshot.HeaderOverrides["Authorization"] != "" || snapshot.RoutingFingerprint == "" {
		t.Fatalf("unsafe or missing direct routing fields: %#v", snapshot)
	}
	serialized, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "direct-api-secret") || strings.Contains(string(serialized), "proxy-password") {
		t.Fatalf("direct export is JSON serializable with secrets: %s", serialized)
	}
}

func TestExportDirectProbeSnapshotPreservesStepUpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer browser-jwt" || r.Header.Get("x-api-key") != "" {
			t.Fatalf("unexpected authorization headers: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "STEP_UP_REQUIRED", "message": "请先完成二次验证"})
	}))
	defer server.Close()

	_, err := newTestClient(t, server.URL).ExportDirectProbeSnapshot(context.Background(), 7, "browser-jwt", ForwardedIdentity{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden || httpErr.Code != "STEP_UP_REQUIRED" || httpErr.Message != "请先完成二次验证" {
		t.Fatalf("unexpected step-up error: %T %#v", err, err)
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(baseURL, "admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success", "data": data}); err != nil {
		t.Fatal(err)
	}
}
