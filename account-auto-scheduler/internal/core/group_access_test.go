package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func accessTestGroups() []model.UpstreamGroup {
	return []model.UpstreamGroup{
		{ID: 1, Name: "A & 共享", IsExclusive: true},
		{ID: 2, Name: "B", IsExclusive: true},
		{ID: 3, Name: "Public"},
	}
}

func TestGroupAccessListUsesExactPermissionAndPagedNameFilter(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/groups/all") {
			writeEnvelope(t, w, accessTestGroups())
			return
		}
		if r.URL.Path != "/api/v1/admin/users" {
			t.Errorf("unexpected route: %s", r.URL)
			w.WriteHeader(404)
			return
		}
		q := r.URL.Query()
		if q.Get("group_name") != "A & 共享" || q.Get("include_subscriptions") != "false" || q.Get("sort_order") != "asc" {
			t.Errorf("query = %s", r.URL)
		}
		calls++
		items := []model.GroupAccessUser{
			{ID: 11, AllowedGroups: []int64{1}, Email: "a@example.test", Status: "disabled"},
			{ID: 12, AllowedGroups: []int64{99}}, // same/sub-string name, different ID
		}
		if calls == 2 {
			items = []model.GroupAccessUser{{ID: 13, AllowedGroups: []int64{1, 2}}}
		}
		writeEnvelope(t, w, pageResponse[model.GroupAccessUser]{Items: items, Total: 3, Page: calls, Pages: 2})
	}))
	defer server.Close()
	result, err := newTestClient(t, server.URL).GetGroupAccessUsers(context.Background(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(result.Users) != 2 || result.Users[0].ID != 11 || result.Users[0].Authorized || !result.Users[1].Authorized {
		t.Fatalf("result = %#v, calls=%d", result, calls)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "allowed_groups") {
		t.Fatal("private permission snapshot leaked")
	}
}

func TestGroupAccessListRejectsIncompleteOrUnboundedData(t *testing.T) {
	for _, mode := range []string{"too-many", "empty-page", "duplicate", "page-failure", "wrong-page", "changed-total"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/groups/all") {
					writeEnvelope(t, w, accessTestGroups())
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				result := pageResponse[model.GroupAccessUser]{Page: page, Pages: 2, Total: 2,
					Items: []model.GroupAccessUser{{ID: 11, AllowedGroups: []int64{1}}}}
				switch mode {
				case "too-many":
					result.Total = 10001
				case "empty-page":
					result.Items = nil
				case "page-failure":
					if page == 2 {
						w.WriteHeader(500)
						return
					}
				case "wrong-page":
					result.Page = 99
				case "changed-total":
					if page == 2 {
						result.Total = 3
					}
				}
				writeEnvelope(t, w, result)
			}))
			defer server.Close()
			result, err := newTestClient(t, server.URL).GetGroupAccessUsers(context.Background(), 2, 1)
			if err == nil || len(result.Users) != 0 {
				t.Fatalf("partial data accepted: %#v %v", result, err)
			}
		})
	}
}

func TestGroupAccessSyncFreshMergeSkipAndReadFailure(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/groups/all") {
			writeEnvelope(t, w, accessTestGroups())
			return
		}
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), 10, 64)
		groups := map[int64][]int64{11: {1, 7, 88}, 12: {1, 2}, 13: {7}}[id]
		if id == 14 {
			w.WriteHeader(404)
			return
		}
		if r.Method == "GET" {
			writeEnvelope(t, w, model.GroupAccessUser{ID: id, AllowedGroups: groups})
			return
		}
		writes++
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 1 || body["allowed_groups"] == nil {
			t.Errorf("changed unrelated user fields: %#v", body)
		}
		var actual []int64
		_ = json.Unmarshal(body["allowed_groups"], &actual)
		if !reflect.DeepEqual(actual, []int64{1, 7, 88, 2}) {
			t.Errorf("lost latest permission: %v", actual)
		}
		writeEnvelope(t, w, model.GroupAccessUser{ID: id, AllowedGroups: actual})
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := client.SyncGroupAccessUsers(context.Background(), 2, 1, []int64{11, 12, 13, 14})
	if err != nil {
		t.Fatal(err)
	}
	statuses := []string{}
	for _, row := range result {
		statuses = append(statuses, row.Status)
	}
	if writes != 1 || !slices.Equal(statuses, []string{"added", "skipped", "skipped", "failed"}) {
		t.Fatalf("unexpected writes=%d results=%#v", writes, result)
	}
}

func TestGroupAccessUnknownWriteStopsWithoutRetry(t *testing.T) {
	for _, mode := range []string{"error", "lost-permission", "empty"} {
		t.Run(mode, func(t *testing.T) {
			writes, reads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/groups/all") {
					writeEnvelope(t, w, accessTestGroups())
					return
				}
				if r.Method == "GET" {
					reads++
					writeEnvelope(t, w, model.GroupAccessUser{ID: 11, AllowedGroups: []int64{1, 9}})
					return
				}
				writes++
				switch mode {
				case "error":
					w.WriteHeader(500)
				case "empty":
					writeEnvelope(t, w, nil)
				case "lost-permission":
					writeEnvelope(t, w, model.GroupAccessUser{ID: 11, AllowedGroups: []int64{2}})
				}
			}))
			defer server.Close()
			result, err := newTestClient(t, server.URL).SyncGroupAccessUsers(context.Background(), 2, 1, []int64{11, 12})
			if err != nil || writes != 1 || reads != 1 || result[0].Status != "unknown" || result[1].Status != "failed" {
				t.Fatalf("unsafe retry/false success: writes=%d reads=%d %#v %v", writes, reads, result, err)
			}
		})
	}
}

func TestGroupAccessValidatesGroupsAndBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/groups/all") {
			writeEnvelope(t, w, accessTestGroups())
			return
		}
		t.Errorf("invalid request reached users: %s", r.URL)
		w.WriteHeader(500)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, test := range []struct {
		target, source int64
		ids            []int64
	}{
		{2, 1, nil}, {2, 1, []int64{0}}, {2, 1, []int64{11, 11}}, {2, 2, []int64{11}},
		{3, 1, []int64{11}}, {2, 3, []int64{11}}, {99, 1, []int64{11}}, {2, 99, []int64{11}},
		{2, 1, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
	} {
		if _, err := client.SyncGroupAccessUsers(context.Background(), test.target, test.source, test.ids); !errors.Is(err, ErrGroupAccessInvalid) {
			t.Errorf("invalid case=%#v err=%v", test, err)
		}
	}
	client.groupAccessSyncMu.Lock()
	defer client.groupAccessSyncMu.Unlock()
	if _, err := client.SyncGroupAccessUsers(context.Background(), 2, 1, []int64{11}); !errors.Is(err, ErrGroupAccessBusy) {
		t.Errorf("overlapping sync not rejected: %v", err)
	}
}
