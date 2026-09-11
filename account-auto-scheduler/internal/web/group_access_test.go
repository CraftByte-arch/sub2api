package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
)

type fakeGroupAccessCore struct {
	fakeAdminCore
	reads, writes  int
	target, source int64
	err            error
}

func (f *fakeGroupAccessCore) GetGroupAccessUsers(_ context.Context, target, source int64) (core.GroupAccessList, error) {
	f.reads++
	f.target, f.source = target, source
	return core.GroupAccessList{Users: []core.GroupAccessEntry{}}, f.err
}
func (f *fakeGroupAccessCore) SyncGroupAccessUsers(_ context.Context, target, source int64, ids []int64) ([]core.GroupAccessSyncResult, error) {
	f.writes++
	f.target, f.source = target, source
	return []core.GroupAccessSyncResult{{UserID: ids[0], Status: "added"}}, f.err
}

func TestGroupAccessRoutesRequireAdmin(t *testing.T) {
	for _, role := range []string{"", "user", "admin"} {
		for _, method := range []string{"GET", "POST"} {
			t.Run(role+method, func(t *testing.T) {
				client := &fakeGroupAccessCore{fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: role}}}
				server := NewServer(nil, client, Options{}, nil)
				path := "/api/groups/2/access-users"
				if method == "POST" {
					path += "/sync"
				}
				r := authenticatedRequest(method, path, `{"source_group_id":1,"user_ids":[11]}`)
				if role == "" {
					r.Header.Del("Authorization")
				}
				w := httptest.NewRecorder()
				server.Handler().ServeHTTP(w, r)
				if role != "admin" {
					if w.Code < 400 || client.reads+client.writes != 0 {
						t.Fatalf("unauthorized access: %d %#v", w.Code, client)
					}
				} else if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("admin route failed: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestGroupAccessRouteInputValidation(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{"GET", "/api/groups/0/access-users", ""},
		{"GET", "/api/groups/2/access-users?source_group_id=2", ""},
		{"GET", "/api/groups/2/access-users?source_group_id=bad", ""},
		{"POST", "/api/groups/2/access-users/sync", `{"source_group_id":1,"user_ids":[]}`},
		{"POST", "/api/groups/2/access-users/sync", `{"source_group_id":1,"user_ids":[11,11]}`},
		{"POST", "/api/groups/2/access-users/sync", `{"source_group_id":1,"user_ids":[-1]}`},
		{"POST", "/api/groups/2/access-users/sync", `{"source_group_id":2,"user_ids":[11]}`},
		{"POST", "/api/groups/2/access-users/sync", `{"source_group_id":1,"user_ids":[11],"group_rates":{}}`},
		{"POST", "/api/groups/2/access-users/sync", `{"source_group_id":1,"user_ids":[11]} {}`},
	}
	for _, test := range cases {
		client := &fakeGroupAccessCore{fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}}
		w := httptest.NewRecorder()
		NewServer(nil, client, Options{}, nil).Handler().ServeHTTP(w, authenticatedRequest(test.method, test.path, test.body))
		if w.Code != 400 || client.reads+client.writes != 0 {
			t.Errorf("invalid request passed: %#v code=%d", test, w.Code)
		}
	}
}

func TestGroupAccessErrorMappingDoesNotLeakUpstreamDetails(t *testing.T) {
	for _, test := range []struct {
		err  error
		code int
	}{
		{core.ErrGroupAccessInvalid, 400}, {core.ErrGroupAccessBusy, 409}, {errors.New("secret database detail"), 502},
	} {
		client := &fakeGroupAccessCore{fakeAdminCore: fakeAdminCore{user: core.AdminUser{ID: 1, Role: "admin"}}, err: test.err}
		w := httptest.NewRecorder()
		NewServer(nil, client, Options{}, nil).Handler().ServeHTTP(w, authenticatedRequest(http.MethodPost,
			"/api/groups/2/access-users/sync", `{"source_group_id":1,"user_ids":[11]}`))
		if w.Code != test.code || strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%d %s", w.Code, w.Body.String())
		}
	}
}
