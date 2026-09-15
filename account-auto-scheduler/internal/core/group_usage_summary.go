package core

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const (
	groupUsageSummaryCacheTTL     = 45 * time.Second
	groupUsageSummaryQueryTimeout = 8 * time.Second
)

type groupUsageSummaryFlight struct {
	done   chan struct{}
	result model.GroupUsageSummarySnapshot
	err    error
}

// GetGroupUsageSummary reads the existing Sub2API group usage summary
// endpoint. The endpoint already combines the daily rollups with the small
// current-day tail, so the sidecar never scans usage_logs itself. Results are
// cached briefly because the overview refreshes more often than the summary
// needs to change.
//
// On a refresh failure, the most recent successful result is returned with
// Stale=true together with the error. This keeps the account console usable
// while making the degraded state visible to the caller and UI.
func (c *Client) GetGroupUsageSummary(ctx context.Context) (model.GroupUsageSummarySnapshot, error) {
	if c == nil {
		return unavailableGroupUsageSummary(time.Now().UTC()), errors.New("Sub2API client is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()

	c.groupUsageMu.Lock()
	if c.groupUsageCached != nil && now.Before(c.groupUsageCachedAt.Add(groupUsageSummaryCacheTTL)) {
		result := cloneGroupUsageSummarySnapshot(*c.groupUsageCached)
		c.groupUsageMu.Unlock()
		return result, nil
	}
	if flight := c.groupUsageFlight; flight != nil {
		c.groupUsageMu.Unlock()
		select {
		case <-flight.done:
			return cloneGroupUsageSummarySnapshot(flight.result), flight.err
		case <-ctx.Done():
			return model.GroupUsageSummarySnapshot{}, ctx.Err()
		}
	}
	flight := &groupUsageSummaryFlight{done: make(chan struct{})}
	c.groupUsageFlight = flight
	lastGood := cloneGroupUsageSummaryPointer(c.groupUsageCached)
	c.groupUsageMu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, groupUsageSummaryQueryTimeout)
	items, err := c.fetchGroupUsageSummary(queryCtx)
	cancel()

	result := model.GroupUsageSummarySnapshot{
		Ready:     err == nil,
		QueriedAt: now,
		Source:    "sub2api_group_usage_rollup",
		Items:     items,
	}
	if err != nil {
		if lastGood != nil && lastGood.Ready {
			result = cloneGroupUsageSummarySnapshot(*lastGood)
			result.Stale = true
			result.Notice = "最近一次成功结果；本次同步失败"
		} else {
			result = unavailableGroupUsageSummary(now)
		}
	}

	c.groupUsageMu.Lock()
	stored := cloneGroupUsageSummarySnapshot(result)
	c.groupUsageCached = &stored
	c.groupUsageCachedAt = now
	flight.result = cloneGroupUsageSummarySnapshot(result)
	flight.err = err
	c.groupUsageFlight = nil
	close(flight.done)
	c.groupUsageMu.Unlock()
	return result, err
}

func (c *Client) fetchGroupUsageSummary(ctx context.Context) ([]model.GroupUsageSummary, error) {
	var items []model.GroupUsageSummary
	if err := c.adminJSON(ctx, http.MethodGet, "/admin/groups/usage-summary", nil, &items); err != nil {
		return nil, err
	}
	return normalizeGroupUsageSummaries(items), nil
}

func normalizeGroupUsageSummaries(items []model.GroupUsageSummary) []model.GroupUsageSummary {
	result := make([]model.GroupUsageSummary, 0, len(items))
	for _, item := range items {
		if item.GroupID <= 0 {
			continue
		}
		if math.IsNaN(item.TodayCost) || math.IsInf(item.TodayCost, 0) {
			item.TodayCost = 0
		}
		if math.IsNaN(item.YesterdayCost) || math.IsInf(item.YesterdayCost, 0) {
			item.YesterdayCost = 0
		}
		if math.IsNaN(item.TotalCost) || math.IsInf(item.TotalCost, 0) {
			item.TotalCost = 0
		}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].GroupID < result[j].GroupID
	})
	return result
}

func unavailableGroupUsageSummary(now time.Time) model.GroupUsageSummarySnapshot {
	return model.GroupUsageSummarySnapshot{
		Ready:     false,
		QueriedAt: now,
		Source:    "sub2api_group_usage_rollup",
		Notice:    "分组今日消耗暂不可用",
		Items:     []model.GroupUsageSummary{},
	}
}

func cloneGroupUsageSummaryPointer(value *model.GroupUsageSummarySnapshot) *model.GroupUsageSummarySnapshot {
	if value == nil {
		return nil
	}
	copy := cloneGroupUsageSummarySnapshot(*value)
	return &copy
}

func cloneGroupUsageSummarySnapshot(value model.GroupUsageSummarySnapshot) model.GroupUsageSummarySnapshot {
	value.Items = append([]model.GroupUsageSummary(nil), value.Items...)
	return value
}
