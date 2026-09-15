package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/core"
)

type groupAccessConsole interface {
	GetGroupAccessUsers(context.Context, int64, int64) (core.GroupAccessList, error)
	SyncGroupAccessUsers(context.Context, int64, int64, []int64) ([]core.GroupAccessSyncResult, error)
}

func (s *Server) handleGroupAccessUsers(w http.ResponseWriter, r *http.Request) {
	client, ok := s.core.(groupAccessConsole)
	if !ok {
		writeError(w, 503, "GROUP_ACCESS_UNAVAILABLE", "分组授权服务不可用")
		return
	}
	target, err := strconv.ParseInt(r.PathValue("groupID"), 10, 64)
	if err != nil || target <= 0 {
		writeGroupAccessError(w, core.ErrGroupAccessInvalid)
		return
	}
	source := target
	if raw, present := r.URL.Query()["source_group_id"]; present {
		if len(raw) != 1 {
			writeGroupAccessError(w, core.ErrGroupAccessInvalid)
			return
		}
		source, err = strconv.ParseInt(raw[0], 10, 64)
		if err != nil || source <= 0 || source == target {
			writeGroupAccessError(w, core.ErrGroupAccessInvalid)
			return
		}
	}
	list, err := client.GetGroupAccessUsers(r.Context(), target, source)
	if err != nil {
		writeGroupAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, list)
}

func (s *Server) handleSyncGroupAccessUsers(w http.ResponseWriter, r *http.Request) {
	client, ok := s.core.(groupAccessConsole)
	if !ok {
		writeError(w, 503, "GROUP_ACCESS_UNAVAILABLE", "分组授权服务不可用")
		return
	}
	target, err := strconv.ParseInt(r.PathValue("groupID"), 10, 64)
	if err != nil || target <= 0 {
		writeGroupAccessError(w, core.ErrGroupAccessInvalid)
		return
	}
	var input struct {
		SourceGroupID int64   `json:"source_group_id"`
		UserIDs       []int64 `json:"user_ids"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeGroupAccessError(w, core.ErrGroupAccessInvalid)
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF || input.SourceGroupID <= 0 || input.SourceGroupID == target ||
		len(input.UserIDs) == 0 || len(input.UserIDs) > core.GroupAccessBatchSize {
		writeGroupAccessError(w, core.ErrGroupAccessInvalid)
		return
	}
	seen := map[int64]bool{}
	for _, id := range input.UserIDs {
		if id <= 0 || seen[id] {
			writeGroupAccessError(w, core.ErrGroupAccessInvalid)
			return
		}
		seen[id] = true
	}
	results, err := client.SyncGroupAccessUsers(r.Context(), target, input.SourceGroupID, input.UserIDs)
	if err != nil {
		writeGroupAccessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	admin, _ := r.Context().Value(adminUserKey).(core.AdminUser)
	s.logger.InfoContext(r.Context(), "sidecar group permission sync",
		"admin_id", admin.ID, "target_group_id", target, "source_group_id", input.SourceGroupID, "results", results)
	writeJSON(w, 200, map[string]any{"results": results})
}

func writeGroupAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrGroupAccessInvalid):
		writeError(w, 400, "INVALID_GROUP_ACCESS", "请选择有效且不同的专属分组；每批选择 1–10 个不同用户")
	case errors.Is(err, core.ErrGroupAccessBusy):
		writeError(w, 409, "GROUP_ACCESS_BUSY", "已有授权同步正在执行，请稍后重试")
	case errors.Is(err, core.ErrGroupAccessReadUnavailable):
		writeError(w, 503, "GROUP_ACCESS_READ_UNAVAILABLE", "侧车未配置专属分组授权只读数据库，请检查数据库连接配置")
	default:
		writeError(w, 502, "GROUP_ACCESS_FAILED", "无法完整获取分组授权数据，请刷新重试或在主后台核对")
	}
}
