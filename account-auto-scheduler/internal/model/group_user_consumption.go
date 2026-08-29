package model

import "time"

// GroupUserConsumptionSnapshot is the administrator-facing, current-day
// ranking for one real Sub2API group. Nil metric pointers intentionally mean
// that the upstream did not provide that value; zero remains a valid value.
type GroupUserConsumptionSnapshot struct {
	GroupID   int64                        `json:"group_id"`
	GroupName string                       `json:"group_name,omitempty"`
	Date      string                       `json:"date"`
	Limit     int                          `json:"limit"`
	QueriedAt time.Time                    `json:"queried_at"`
	Partial   bool                         `json:"partial"`
	Notice    string                       `json:"notice,omitempty"`
	Users     []GroupUserConsumptionRecord `json:"users"`
}

// GroupUserConsumptionRecord contains one user's current-day consumption in
// the selected group, using Sub2API's actual-cost (user billing) semantics.
type GroupUserConsumptionRecord struct {
	UserID      int64    `json:"user_id"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email,omitempty"`
	ActualCost  *float64 `json:"actual_cost"`
	Requests    *int64   `json:"requests"`
	TotalTokens *int64   `json:"total_tokens"`
}
