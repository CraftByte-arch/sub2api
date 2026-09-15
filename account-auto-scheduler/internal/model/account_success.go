package model

import (
	"fmt"
	"time"
)

const AccountSuccessLowSampleThreshold int64 = 20

type AccountSuccessWindow struct {
	SuccessCount        int64      `json:"success_count"`
	EffectiveAttempts   int64      `json:"effective_attempts"`
	AttemptCount        int64      `json:"attempt_count"`
	ClientCanceledCount int64      `json:"client_canceled_count"`
	FailoverCount       int64      `json:"failover_count"`
	Rate                float64    `json:"rate"`
	LowSample           bool       `json:"low_sample"`
	WindowMinutes       int        `json:"window_minutes"`
	DataThrough         *time.Time `json:"data_through,omitempty"`
	LastObservedAt      *time.Time `json:"last_observed_at,omitempty"`
}

type GroupAccountSuccessRate struct {
	GroupID   int64                `json:"group_id"`
	AccountID int64                `json:"account_id"`
	Recent    AccountSuccessWindow `json:"recent"`
	Reference AccountSuccessWindow `json:"reference_24h"`
}

type AccountSuccessSnapshot struct {
	Ready              bool                                `json:"ready"`
	Partial            bool                                `json:"partial"`
	Stale              bool                                `json:"stale"`
	QueriedAt          time.Time                           `json:"queried_at"`
	DataThrough        *time.Time                          `json:"data_through,omitempty"`
	ReferenceThrough   *time.Time                          `json:"reference_through,omitempty"`
	Source             string                              `json:"source"`
	Notice             string                              `json:"notice,omitempty"`
	ActiveAccountCount int                                 `json:"active_account_count"`
	Items              []GroupAccountSuccessRate           `json:"items"`
	CollectionHealth   *AccountPerformanceCollectionHealth `json:"collection_health,omitempty"`
}

type AccountPerformanceCollectionHealth struct {
	Status                string     `json:"status"`
	DroppedSamples        int64      `json:"dropped_samples"`
	PendingSamples        int64      `json:"pending_samples"`
	LastSuccessfulFlushAt *time.Time `json:"last_successful_flush_at,omitempty"`
}

func GroupAccountSuccessKey(groupID, accountID int64) string {
	return fmt.Sprintf("%d:%d", groupID, accountID)
}

func (w *AccountSuccessWindow) Finalize() {
	if w == nil {
		return
	}
	if w.EffectiveAttempts < 0 {
		w.EffectiveAttempts = 0
	}
	if w.SuccessCount < 0 {
		w.SuccessCount = 0
	}
	if w.SuccessCount > w.EffectiveAttempts {
		w.SuccessCount = w.EffectiveAttempts
	}
	if w.EffectiveAttempts > 0 {
		w.Rate = float64(w.SuccessCount) / float64(w.EffectiveAttempts)
	}
	w.LowSample = w.EffectiveAttempts > 0 && w.EffectiveAttempts < AccountSuccessLowSampleThreshold
}
