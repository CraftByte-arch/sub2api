package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type usageCardSummaryRepoStub struct {
	UsageCardRepository
	cards []UserUsageCard
	err   error
	now   time.Time
}

type usageCardSummarySettingRepoStub struct {
	values map[string]string
}

func (s usageCardSummarySettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	return nil, errors.New("not implemented")
}

func (s usageCardSummarySettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", errors.New("not found")
}

func (s usageCardSummarySettingRepoStub) Set(ctx context.Context, key, value string) error {
	return errors.New("not implemented")
}

func (s usageCardSummarySettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	return nil, errors.New("not implemented")
}

func (s usageCardSummarySettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	return errors.New("not implemented")
}

func (s usageCardSummarySettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	return nil, errors.New("not implemented")
}

func (s usageCardSummarySettingRepoStub) Delete(ctx context.Context, key string) error {
	return errors.New("not implemented")
}

func (s *usageCardSummaryRepoStub) ListAvailableCards(ctx context.Context, userID int64, now time.Time) ([]UserUsageCard, error) {
	s.now = now
	return s.cards, s.err
}

func TestUsageCardServiceGetMySummaryCountsAndSumsAvailableCards(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	repo := &usageCardSummaryRepoStub{cards: []UserUsageCard{
		{ID: 1, TotalLimitUSD: 10, UsedUSD: 2},
		{ID: 2, TotalLimitUSD: 5.5, UsedUSD: 1.25},
	}}
	settings := usageCardSummarySettingRepoStub{values: map[string]string{
		SettingKeyUsageCardEnabled: "true",
	}}
	svc := NewUsageCardService(repo, settings)

	summary, err := svc.GetMySummary(context.Background(), 42, now)

	require.NoError(t, err)
	require.Equal(t, 2, summary.AvailableCount)
	require.InDelta(t, 12.25, summary.AvailableRemainingUSD, 0.000001)
	require.Equal(t, now, repo.now)
}
