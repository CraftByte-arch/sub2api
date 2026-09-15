package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

var (
	ErrGroupAccessInvalid         = errors.New("请选择有效且不同的专属分组")
	ErrGroupAccessBusy            = errors.New("已有用户授权同步正在执行，请稍后重试")
	ErrGroupAccessReadUnavailable = errors.New("未配置专属分组授权只读数据库")
)

const GroupAccessBatchSize = 10

type GroupAccessEntry = model.GroupAccessEntry

type GroupAccessReader interface {
	ListGroupAccessUsers(context.Context, int64, int64) ([]model.GroupAccessEntry, error)
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

func (c *Client) SetGroupAccessReader(reader GroupAccessReader) {
	c.groupAccessReader = reader
}

// Group metadata and all permission mutations use the administrator HTTP API.
// Membership reads use the sidecar's optional read-only database connection so
// this dialog never invokes the general user-list endpoint and its usage-log
// enrichment queries.
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	target, source, err := c.accessGroups(ctx, targetID, sourceID)
	if err != nil {
		return GroupAccessList{}, err
	}
	if c.groupAccessReader == nil {
		return GroupAccessList{}, ErrGroupAccessReadUnavailable
	}
	users, err := c.groupAccessReader.ListGroupAccessUsers(ctx, targetID, sourceID)
	if err != nil {
		return GroupAccessList{}, err
	}
	if len(users) > 10000 {
		return GroupAccessList{}, errors.New("用户数量超过读取上限，请在主后台处理")
	}
	seen := make(map[int64]struct{}, len(users))
	for _, user := range users {
		if user.ID <= 0 {
			return GroupAccessList{}, errors.New("授权用户数据无效，请刷新重试")
		}
		if _, exists := seen[user.ID]; exists {
			return GroupAccessList{}, errors.New("授权用户数据重复，请刷新重试")
		}
		seen[user.ID] = struct{}{}
	}
	return GroupAccessList{Group: target, SourceGroup: source, Users: users}, nil
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
