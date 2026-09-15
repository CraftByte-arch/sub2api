package core

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	groupUserConsumptionLimit   = 20
	groupUserConsumptionTimeout = 12 * time.Second
)

type groupUserBreakdownResponse struct {
	Users []groupUserBreakdownRow `json:"users"`
}

// Pointer fields distinguish an upstream value of zero from a field that was
// omitted or returned as null. This lets the UI explain partial data instead
// of silently presenting an invented zero.
type groupUserBreakdownRow struct {
	UserID      int64    `json:"user_id"`
	Email       string   `json:"email"`
	Username    string   `json:"username"`
	ActualCost  *float64 `json:"actual_cost"`
	Requests    *int64   `json:"requests"`
	TotalTokens *int64   `json:"total_tokens"`
}

// GetGroupUserConsumption returns today's top users for one real group. The
// date range, sort column, and result limit are deliberately fixed here so a
// browser cannot widen the query or change the billing semantics.
func (c *Client) GetGroupUserConsumption(ctx context.Context, groupID int64) (model.GroupUserConsumptionSnapshot, error) {
	if groupID <= 0 {
		return model.GroupUserConsumptionSnapshot{}, errors.New("group ID must be positive")
	}
	queryCtx, cancel := context.WithTimeout(ctx, groupUserConsumptionTimeout)
	defer cancel()
	return c.getGroupUserConsumptionAt(queryCtx, groupID, time.Now())
}

func (c *Client) getGroupUserConsumptionAt(ctx context.Context, groupID int64, now time.Time) (model.GroupUserConsumptionSnapshot, error) {
	if groupID <= 0 {
		return model.GroupUserConsumptionSnapshot{}, errors.New("group ID must be positive")
	}
	date := now.Format("2006-01-02")
	query := url.Values{}
	query.Set("start_date", date)
	query.Set("end_date", date)
	query.Set("group_id", strconv.FormatInt(groupID, 10))
	query.Set("limit", strconv.Itoa(groupUserConsumptionLimit))
	query.Set("sort_by", "actual_cost")

	var response groupUserBreakdownResponse
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/dashboard/user-breakdown?"+query.Encode(), nil, &response); err != nil {
		return model.GroupUserConsumptionSnapshot{}, err
	}

	rows, partial, notice := normalizeGroupUserBreakdown(response.Users)
	if len(rows) > groupUserConsumptionLimit {
		rows = rows[:groupUserConsumptionLimit]
	}
	return model.GroupUserConsumptionSnapshot{
		GroupID:   groupID,
		Date:      date,
		Limit:     groupUserConsumptionLimit,
		QueriedAt: now.UTC(),
		Partial:   partial,
		Notice:    notice,
		Users:     rows,
	}, nil
}

func normalizeGroupUserBreakdown(input []groupUserBreakdownRow) ([]model.GroupUserConsumptionRecord, bool, string) {
	rows := make([]model.GroupUserConsumptionRecord, 0, min(len(input), groupUserConsumptionLimit))
	seen := make(map[int64]struct{}, len(input))
	partial := false
	invalidIDs := 0
	duplicateIDs := 0
	missingMetrics := 0
	for _, item := range input {
		if item.UserID <= 0 {
			partial = true
			invalidIDs++
			continue
		}
		if _, exists := seen[item.UserID]; exists {
			partial = true
			duplicateIDs++
			continue
		}
		seen[item.UserID] = struct{}{}

		cost := normalizeNonNegativeFloat(item.ActualCost)
		requests := normalizeNonNegativeInt(item.Requests)
		tokens := normalizeNonNegativeInt(item.TotalTokens)
		missing := 0
		if cost == nil {
			missing++
		}
		if requests == nil {
			missing++
		}
		if tokens == nil {
			missing++
		}
		if missing > 0 {
			partial = true
			missingMetrics += missing
		}

		email := strings.TrimSpace(item.Email)
		username := strings.TrimSpace(item.Username)
		displayName := firstNonEmpty(username, email, "用户 #"+strconv.FormatInt(item.UserID, 10))
		rows = append(rows, model.GroupUserConsumptionRecord{
			UserID:      item.UserID,
			DisplayName: displayName,
			Email:       email,
			ActualCost:  cost,
			Requests:    requests,
			TotalTokens: tokens,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if comparison := compareOptionalFloatDesc(rows[i].ActualCost, rows[j].ActualCost); comparison != 0 {
			return comparison < 0
		}
		if comparison := compareOptionalIntDesc(rows[i].TotalTokens, rows[j].TotalTokens); comparison != 0 {
			return comparison < 0
		}
		if comparison := compareOptionalIntDesc(rows[i].Requests, rows[j].Requests); comparison != 0 {
			return comparison < 0
		}
		return rows[i].UserID < rows[j].UserID
	})

	notices := make([]string, 0, 3)
	if invalidIDs > 0 {
		notices = append(notices, strconv.Itoa(invalidIDs)+" 行用户 ID 无效，已忽略")
	}
	if duplicateIDs > 0 {
		notices = append(notices, strconv.Itoa(duplicateIDs)+" 行重复用户记录，已忽略")
	}
	if missingMetrics > 0 {
		notices = append(notices, "部分用户的消耗、请求数或 Token 数暂不可用")
	}
	return rows, partial, strings.Join(notices, "；")
}

func normalizeNonNegativeFloat(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return nil
	}
	copy := *value
	return &copy
}

func normalizeNonNegativeInt(value *int64) *int64 {
	if value == nil || *value < 0 {
		return nil
	}
	copy := *value
	return &copy
}

func compareOptionalFloatDesc(left, right *float64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if *left > *right {
		return -1
	}
	if *left < *right {
		return 1
	}
	return 0
}

func compareOptionalIntDesc(left, right *int64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if *left > *right {
		return -1
	}
	if *left < *right {
		return 1
	}
	return 0
}
