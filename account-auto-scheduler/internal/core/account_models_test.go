package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAvailableModelsUsesExistingAdminEndpointAndNormalizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/accounts/42/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("x-api-key") != "admin-secret" {
			t.Fatalf("missing administrator key: %#v", r.Header)
		}
		writeEnvelope(t, w, []map[string]any{
			{"id": " gpt-real ", "display_name": " Real GPT "},
			{"id": "gpt-real", "display_name": "duplicate"},
			{"id": "claude-real"},
			{"id": "   "},
		})
	}))
	defer server.Close()

	models, err := newTestClient(t, server.URL).GetAvailableModels(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "gpt-real" || models[0].DisplayName != "Real GPT" || models[1].DisplayName != "claude-real" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestGetAvailableModelsRejectsInvalidAccount(t *testing.T) {
	client := newTestClient(t, "http://example.test")
	if _, err := client.GetAvailableModels(context.Background(), 0); err == nil {
		t.Fatal("invalid account ID was accepted")
	}
}
