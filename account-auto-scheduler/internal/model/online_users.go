package model

import "time"

// OnlineUsersSnapshot is the sidecar's short-lived administrator view of
// users who made a request during the configured online window.
type OnlineUsersSnapshot struct {
	Count                int           `json:"count"`
	WindowMinutes        int           `json:"window_minutes"`
	QueriedAt            time.Time     `json:"queried_at"`
	Source               string        `json:"source"`
	Partial              bool          `json:"partial"`
	Truncated            bool          `json:"truncated"`
	Notice               string        `json:"notice,omitempty"`
	GroupCounts          map[int64]int `json:"group_counts"`
	GroupCountsAvailable bool          `json:"group_counts_available"`
	GroupCountsPartial   bool          `json:"group_counts_partial"`
	Users                []OnlineUser  `json:"users"`
}

// OnlineUser is a non-sensitive administrator display row. A nil TodayCost
// or TodayTokens means the upstream usage summary was unavailable; zero is a
// valid measured value and is therefore represented by a non-nil pointer.
type OnlineUser struct {
	ID            int64     `json:"id"`
	DisplayName   string    `json:"display_name"`
	Email         string    `json:"email,omitempty"`
	LastCallAt    time.Time `json:"last_call_at"`
	TodayCost     *float64  `json:"today_cost"`
	TodayTokens   *int64    `json:"today_tokens"`
	TodayRequests *int64    `json:"today_requests"`
}
