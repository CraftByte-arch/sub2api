package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/collection"
)

func TestProvideTimingWheelService_ReturnsError(t *testing.T) {
	original := newTimingWheel
	t.Cleanup(func() { newTimingWheel = original })

	newTimingWheel = func(_ time.Duration, _ int, _ collection.Execute) (*collection.TimingWheel, error) {
		return nil, errors.New("boom")
	}

	svc, err := ProvideTimingWheelService()
	if err == nil {
		t.Fatalf("期望返回 error，但得到 nil")
	}
	if svc != nil {
		t.Fatalf("期望返回 nil svc，但得到非空")
	}
}

func TestProvideImageStudioJobServiceSharesHealthAndDoesNotFailStartupProbe(t *testing.T) {
	storage := &imageStudioWireProbeStorage{err: errors.New("open /private/shared-data: permission denied")}
	health := ProvideImageStudioInputStorageHealth(storage)

	svc := ProvideImageStudioJobService(
		&imageStudioJobDeleteRepoStub{},
		nil,
		storage,
		health,
		nil,
		nil,
		nil,
		nil,
	)
	t.Cleanup(svc.Stop)

	require.Same(t, storage, svc.inputStore)
	require.Same(t, health, svc.inputStorageHealth)
	require.Equal(t, 1, storage.calls)
	require.False(t, health.Available())
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, health.Snapshot().LastErrorKind)
}

type imageStudioWireProbeStorage struct {
	ImageStudioInputStorage
	err   error
	calls int
}

func (s *imageStudioWireProbeStorage) Probe(context.Context) error {
	s.calls++
	return s.err
}

func TestProvideTimingWheelService_Success(t *testing.T) {
	svc, err := ProvideTimingWheelService()
	if err != nil {
		t.Fatalf("期望 err 为 nil，但得到: %v", err)
	}
	if svc == nil {
		t.Fatalf("期望 svc 非空，但得到 nil")
	}
	svc.Stop()
}
