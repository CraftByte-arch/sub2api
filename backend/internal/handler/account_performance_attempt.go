package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// accountPerformanceAttempt records exactly one selected-account forward. It
// deliberately owns no failover decisions: callers finish it after the
// existing forward path has selected the final attempt error.
type accountPerformanceAttempt struct {
	recorder service.AccountPerformanceRecorder
	started  time.Time
	delta    service.AccountPerformanceDelta
}

func beginAccountPerformanceAttempt(
	recorder service.AccountPerformanceRecorder,
	accountID int64,
	platform string,
	groupID int64,
	protocol service.FirstTokenProtocol,
	model string,
	switchCount int,
) *accountPerformanceAttempt {
	if recorder == nil || accountID <= 0 || platform == "" || groupID < 0 || protocol == "" {
		return nil
	}
	started := time.Now()
	return &accountPerformanceAttempt{
		recorder: recorder,
		started:  started,
		delta: service.AccountPerformanceDelta{
			BucketStart: started, AccountID: accountID, Platform: platform, GroupID: groupID,
			Protocol: string(protocol), Model: model, AttemptCount: 1, Failover: switchCount > 0,
		},
	}
}

func (a *accountPerformanceAttempt) Finish(parent context.Context, err error, result any) {
	if a == nil || a.recorder == nil {
		return
	}
	delta := a.delta
	delta.Outcome = service.ClassifyAccountPerformanceOutcome(err, parent)
	if delta.Outcome == service.AccountPerformanceOutcomeSuccess {
		duration := time.Since(a.started).Milliseconds()
		delta.DurationMS = &duration
		delta.TTFTMS = accountPerformanceFirstTokenMS(result)
	}
	a.recorder.Record(delta)
}

func accountPerformanceFirstTokenMS(result any) *int64 {
	switch value := result.(type) {
	case *service.ForwardResult:
		if value != nil {
			return accountPerformanceMilliseconds(value.FirstTokenMs)
		}
	case *service.OpenAIForwardResult:
		if value != nil {
			return accountPerformanceMilliseconds(value.FirstTokenMs)
		}
	}
	return nil
}

func accountPerformanceMilliseconds(value *int) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}
