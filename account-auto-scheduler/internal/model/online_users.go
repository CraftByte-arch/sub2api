package model

import "time"

// OnlineUsersSummary is the compact administrator view used for the site-wide
// and per-group online indicators. Ready distinguishes a measured zero from an
// unavailable aggregate source. DataThrough is the exclusive end of the ten-
// minute aggregate window and may trail wall clock time.
type OnlineUsersSummary struct {
	Ready                 bool          `json:"ready"`
	Count                 int           `json:"count"`
	WindowMinutes         int           `json:"window_minutes"`
	QueriedAt             time.Time     `json:"queried_at"`
	DataThrough           *time.Time    `json:"data_through,omitempty"`
	AggregationLagSeconds int64         `json:"aggregation_lag_seconds"`
	Source                string        `json:"source"`
	Partial               bool          `json:"partial"`
	Stale                 bool          `json:"stale"`
	Notice                string        `json:"notice,omitempty"`
	GroupCounts           map[int64]int `json:"group_counts"`
	GroupCountsAvailable  bool          `json:"group_counts_available"`
	GroupCountsPartial    bool          `json:"group_counts_partial"`
}

// OnlineUsersSnapshot is the sidecar's short-lived administrator view of
// users who made a request during the configured online window.
type OnlineUsersSnapshot struct {
	Ready                 bool          `json:"ready"`
	Count                 int           `json:"count"`
	WindowMinutes         int           `json:"window_minutes"`
	QueriedAt             time.Time     `json:"queried_at"`
	DataThrough           *time.Time    `json:"data_through,omitempty"`
	AggregationLagSeconds int64         `json:"aggregation_lag_seconds"`
	Source                string        `json:"source"`
	Partial               bool          `json:"partial"`
	Stale                 bool          `json:"stale"`
	Notice                string        `json:"notice,omitempty"`
	GroupCounts           map[int64]int `json:"group_counts"`
	GroupCountsAvailable  bool          `json:"group_counts_available"`
	GroupCountsPartial    bool          `json:"group_counts_partial"`
	Users                 []OnlineUser  `json:"users"`
}

// OnlineUser is a non-sensitive administrator display row. A nil TodayCost
// or TodayTokens means the upstream usage summary was unavailable; zero is a
// valid measured value and is therefore represented by a non-nil pointer.
type OnlineUser struct {
	ID            int64     `json:"id"`
	DisplayName   string    `json:"display_name"`
	Email         string    `json:"email,omitempty"`
	GroupIDs      []int64   `json:"group_ids"`
	LastCallAt    time.Time `json:"last_call_at"`
	TodayCost     *float64  `json:"today_cost"`
	TodayTokens   *int64    `json:"today_tokens"`
	TodayRequests *int64    `json:"today_requests"`
}

// Summary returns the compact projection of a detail snapshot.
func (s OnlineUsersSnapshot) Summary() OnlineUsersSummary {
	return OnlineUsersSummary{
		Ready:                 s.Ready,
		Count:                 s.Count,
		WindowMinutes:         s.WindowMinutes,
		QueriedAt:             s.QueriedAt,
		DataThrough:           cloneOnlineTime(s.DataThrough),
		AggregationLagSeconds: s.AggregationLagSeconds,
		Source:                s.Source,
		Partial:               s.Partial,
		Stale:                 s.Stale,
		Notice:                s.Notice,
		GroupCounts:           cloneOnlineGroupCounts(s.GroupCounts),
		GroupCountsAvailable:  s.GroupCountsAvailable,
		GroupCountsPartial:    s.GroupCountsPartial,
	}
}

// WithUsers builds a detail snapshot from a compact summary.
func (s OnlineUsersSummary) WithUsers(users []OnlineUser) OnlineUsersSnapshot {
	return OnlineUsersSnapshot{
		Ready:                 s.Ready,
		Count:                 s.Count,
		WindowMinutes:         s.WindowMinutes,
		QueriedAt:             s.QueriedAt,
		DataThrough:           cloneOnlineTime(s.DataThrough),
		AggregationLagSeconds: s.AggregationLagSeconds,
		Source:                s.Source,
		Partial:               s.Partial,
		Stale:                 s.Stale,
		Notice:                s.Notice,
		GroupCounts:           cloneOnlineGroupCounts(s.GroupCounts),
		GroupCountsAvailable:  s.GroupCountsAvailable,
		GroupCountsPartial:    s.GroupCountsPartial,
		Users:                 cloneOnlineUsers(users),
	}
}

func cloneOnlineTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneOnlineGroupCounts(values map[int64]int) map[int64]int {
	if values == nil {
		return nil
	}
	copy := make(map[int64]int, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func cloneOnlineUsers(values []OnlineUser) []OnlineUser {
	if values == nil {
		return nil
	}
	copy := make([]OnlineUser, len(values))
	for i, value := range values {
		copy[i] = value
		copy[i].GroupIDs = append([]int64(nil), value.GroupIDs...)
		if value.TodayCost != nil {
			v := *value.TodayCost
			copy[i].TodayCost = &v
		}
		if value.TodayTokens != nil {
			v := *value.TodayTokens
			copy[i].TodayTokens = &v
		}
		if value.TodayRequests != nil {
			v := *value.TodayRequests
			copy[i].TodayRequests = &v
		}
	}
	return copy
}
