//go:build integration

package service

import "time"

type imageStudioIntegrationCleanupTicker struct {
	ticks <-chan time.Time
}

func (t imageStudioIntegrationCleanupTicker) C() <-chan time.Time { return t.ticks }
func (imageStudioIntegrationCleanupTicker) Stop()                 {}

// SetCleanupTicksForIntegration drives the production cleanup loop without waiting for its real interval.
func (s *ImageStudioJobService) SetCleanupTicksForIntegration(ticks <-chan time.Time) {
	if s == nil {
		return
	}
	s.newCleanupTicker = func(time.Duration) imageStudioInputStorageHealthTicker {
		return imageStudioIntegrationCleanupTicker{ticks: ticks}
	}
}
