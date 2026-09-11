package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

var (
	ErrGroupAccessInvalid = errors.New("请选择有效且不同的专属分组")
	ErrGroupAccessBusy    = errors.New("已有用户授权同步正在执行，请稍后重试")
)

const GroupAccessBatchSize = 10

type GroupAccessEntry struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Status     string `json:"status"`
	Authorized bool   `json:"authorized"`
}

type GroupAccessList struct {
	Group       model.UpstreamGroup `json:"group"`
	SourceGroup model.UpstreamGroup `json:"source_group"`
	Users       []GroupAccessEntry  `json:"users"`
}

type GroupAccessSyncResult struct {
	UserID  int64  `json:"user_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Both reads and writes use the existing admin HTTP API, never the database.
func (c *Client) accessGroups(ctx context.Context, targetID, sourceID int64) (model.UpstreamGroup, model.UpstreamGroup, error) {
	var target, source model.UpstreamGroup
	if targetID <= 0 || sourceID <= 0 {
		return target, source, ErrGroupAccessInvalid
	}
	groups, err := c.ListGroups(ctx)
	if err != nil {
		return target, source, err
	}
	for _, group := range groups {
		if group.ID == targetID {
			target = group
		}
		if group.ID == sourceID {
			source = group
		}
	}
	if !target.IsExclusive || !source.IsExclusive {
		return target, source, ErrGroupAccessInvalid
	}
	return target, source, nil
}

func (c *Client) GetGroupAccessUsers(ctx context.Context, targetID, sourceID int64) (GroupAccessList, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	target, source, err := c.accessGroups(ctx, targetID, sourceID)
	if err != nil {
		return GroupAccessList{}, err
	}
	out := GroupAccessList{Group: target, SourceGroup: source, Users: []GroupAccessEntry{}}
	seen := map[int64]bool{}
	scanned := 0
	var expectedTotal int64 = -1
	// group_name is only a narrowing filter. Exact allowed_groups below is
	// authoritative even for duplicate names and substring matches.
	for page := 1; page <= 100; page++ {
		query := url.Values{
			"page": {strconv.Itoa(page)}, "page_size": {"100"},
			"include_subscriptions": {"false"}, "group_name": {strings.TrimSpace(source.Name)},
			"sort_by": {"created_at"}, "sort_order": {"asc"},
		}
		var result pageResponse[model.GroupAccessUser]
		if err := c.adminJSON(ctx, http.MethodGet, "/admin/users?"+query.Encode(), nil, &result); err != nil {
			return GroupAccessList{}, err
		}
		if result.Total > 10000 || result.Page != page || result.Total < 0 || (len(result.Items) == 0 && int64(scanned) < result.Total) {
			return GroupAccessList{}, errors.New("用户列表过大或分页不完整，请在主后台处理或重试")
		}
		if expectedTotal >= 0 && expectedTotal != result.Total {
			return GroupAccessList{}, errors.New("用户总数在读取期间发生变化，请刷新重试")
		}
		expectedTotal = result.Total
		scanned += len(result.Items)
		if int64(scanned) > result.Total {
			return GroupAccessList{}, errors.New("用户分页数据不一致，请重试")
		}
		for _, user := range result.Items {
			if user.ID <= 0 || seen[user.ID] {
				return GroupAccessList{}, errors.New("用户列表在读取期间发生变化，请刷新重试")
			}
			seen[user.ID] = true
			if slices.Contains(user.AllowedGroups, sourceID) {
				out.Users = append(out.Users, GroupAccessEntry{
					ID: user.ID, Username: user.Username, Email: user.Email, Status: user.Status,
					Authorized: slices.Contains(user.AllowedGroups, targetID),
				})
			}
		}
		if int64(scanned) >= result.Total {
			return out, nil
		}
		if result.Pages > 0 && page >= result.Pages {
			return GroupAccessList{}, errors.New("用户分页数据不完整，请重试")
		}
	}
	return GroupAccessList{}, errors.New("用户数量超过读取上限，请在主后台处理")
}

func (c *Client) SyncGroupAccessUsers(ctx context.Context, targetID, sourceID int64, ids []int64) ([]GroupAccessSyncResult, error) {
	if targetID == sourceID || len(ids) == 0 || len(ids) > GroupAccessBatchSize {
		return nil, ErrGroupAccessInvalid
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, ErrGroupAccessInvalid
		}
		seen[id] = true
	}
	// Serializes sidecar sync requests only; the main-service UI has no shared
	// conditional-update API, so concurrent main-UI edits cannot be locked here.
	if !c.groupAccessSyncMu.TryLock() {
		return nil, ErrGroupAccessBusy
	}
	defer c.groupAccessSyncMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if _, _, err := c.accessGroups(ctx, targetID, sourceID); err != nil {
		return nil, err
	}
	results := make([]GroupAccessSyncResult, 0, len(ids))
	stopped := false
	for _, id := range ids {
		row := GroupAccessSyncResult{UserID: id, Status: "failed", Message: "尚未执行，请刷新核对后重试"}
		if stopped || ctx.Err() != nil {
			results = append(results, row)
			continue
		}
		path := "/admin/users/" + strconv.FormatInt(id, 10)
		var user model.GroupAccessUser
		if err := c.adminJSON(ctx, http.MethodGet, path, nil, &user); err != nil || user.ID != id {
			row.Message = "读取用户最新权限失败，未修改"
			results = append(results, row)
			continue
		}
		switch {
		case !slices.Contains(user.AllowedGroups, sourceID):
			row.Status, row.Message = "skipped", "已无来源分组权限，未修改"
		case slices.Contains(user.AllowedGroups, targetID):
			row.Status, row.Message = "skipped", "已拥有目标分组权限"
		default:
			expected := append(slices.Clone(user.AllowedGroups), targetID)
			var updated model.GroupAccessUser
			err := c.adminJSON(ctx, http.MethodPut, path, map[string]any{"allowed_groups": expected}, &updated)
			if err != nil || updated.ID != id || !sameAccessGroups(expected, updated.AllowedGroups) {
				// Even a non-2xx may follow a committed update. Never pretend a
				// failed response proves nothing changed and never auto-retry.
				row.Status, row.Message = "unknown", "更新结果未确认，请刷新核对权限；未自动重试"
				stopped = true
			} else {
				row.Status, row.Message = "added", fmt.Sprintf("已追加分组 #%d 权限", targetID)
			}
		}
		results = append(results, row)
	}
	return results, nil
}

func sameAccessGroups(a, b []int64) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}
