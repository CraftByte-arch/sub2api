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

type fakeGroupAccessReader struct {
	users          []model.GroupAccessEntry
	err            error
	calls          int
	target, source int64
}

func (f *fakeGroupAccessReader) ListGroupAccessUsers(_ context.Context, target, source int64) ([]model.GroupAccessEntry, error) {
	f.calls++
	f.target, f.source = target, source
	return append([]model.GroupAccessEntry(nil), f.users...), f.err
}

func TestGroupAccessListUsesReadOnlyReaderAndExactIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("unexpected mutation: %s", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/groups/all") {
			writeEnvelope(t, w, accessTestGroups())
			return
		}
		t.Errorf("group-access read unexpectedly used admin HTTP: %s", r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	reader := &fakeGroupAccessReader{users: []model.GroupAccessEntry{
		{ID: 11, Email: "a@example.test", Status: "disabled"},
		{ID: 13, Username: "thirteen", Status: "active", Authorized: true},
	}}
	client := newTestClient(t, server.URL)
	client.SetGroupAccessReader(reader)
	result, err := client.GetGroupAccessUsers(context.Background(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 || reader.target != 2 || reader.source != 1 || len(result.Users) != 2 || result.Users[0].Authorized || !result.Users[1].Authorized {
		t.Fatalf("result = %#v, reader=%#v", result, reader)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "allowed_groups") {
		t.Fatal("private permission snapshot leaked")
	}
}

func TestGroupAccessListRejectsUnavailableOrInvalidReaderData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, accessTestGroups())
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	if _, err := client.GetGroupAccessUsers(context.Background(), 2, 1); !errors.Is(err, ErrGroupAccessReadUnavailable) {
		t.Fatalf("missing reader error = %v", err)
	}

	for name, users := range map[string][]model.GroupAccessEntry{
		"invalid-id": {{ID: 0}},
		"duplicate":  {{ID: 11}, {ID: 11}},
		"too-many":   make([]model.GroupAccessEntry, 10001),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "too-many" {
				for index := range users {
					users[index].ID = int64(index + 1)
				}
			}
			client.SetGroupAccessReader(&fakeGroupAccessReader{users: users})
			result, err := client.GetGroupAccessUsers(context.Background(), 2, 1)
			if err == nil || len(result.Users) != 0 {
				t.Fatalf("invalid reader data accepted: %#v %v", result, err)
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
