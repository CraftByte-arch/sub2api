package model

import "time"

// GroupUsageSummary is the compact, administrator-facing usage projection
// returned by Sub2API's existing group usage summary endpoint. TodayCost uses
// Sub2API's actual-cost (user billing) semantics.
type GroupUsageSummary struct {
	GroupID       int64   `json:"group_id"`
	TodayCost     float64 `json:"today_cost"`
	YesterdayCost float64 `json:"yesterday_cost"`
	TotalCost     float64 `json:"total_cost"`
}

// GroupUsageSummarySnapshot describes the latest bounded summary read. A
// stale snapshot is still useful for the console, but the UI can distinguish
// it from a fresh result instead of displaying an invented zero.
type GroupUsageSummarySnapshot struct {
	Ready     bool                `json:"ready"`
	Stale     bool                `json:"stale"`
	QueriedAt time.Time           `json:"queried_at"`
	Source    string              `json:"source"`
	Notice    string              `json:"notice,omitempty"`
	Items     []GroupUsageSummary `json:"items"`
}
