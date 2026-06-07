package service

import (
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrUsageCardNotFound     = infraerrors.NotFound("USAGE_CARD_NOT_FOUND", "usage card not found")
	ErrUsageCardPlanNotFound = infraerrors.NotFound("USAGE_CARD_PLAN_NOT_FOUND", "usage card plan not found")
	ErrUsageCardUnavailable  = infraerrors.Forbidden("USAGE_CARD_UNAVAILABLE", "usage card is not available")
)

type UsageCardPlan struct {
	ID           int64
	Name         string
	Description  string
	Price        float64
	AmountUSD    float64
	ValidityDays int
	Features     string
	ForSale      bool
	SortOrder    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserUsageCard struct {
	ID               int64
	UserID           int64
	User             *UsageCardUser
	PlanID           *int64
	Name             string
	StartsAt         time.Time
	ExpiresAt        time.Time
	TotalLimitUSD    float64
	UsedUSD          float64
	Status           string
	Source           string
	SourceOrderID    *int64
	SourceRedeemCode *string
	AssignedBy       *int64
	Notes            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type UsageCardUser struct {
	ID       int64
	Email    string
	Username string
}

func (c *UserUsageCard) RemainingUSD() float64 {
	if c == nil {
		return 0
	}
	return c.TotalLimitUSD - c.UsedUSD
}

func (c *UserUsageCard) IsAvailableAt(now time.Time) bool {
	if c == nil {
		return false
	}
	if c.Status != UsageCardStatusActive {
		return false
	}
	if c.DeletedAt != nil {
		return false
	}
	if now.Before(c.StartsAt) || !now.Before(c.ExpiresAt) {
		return false
	}
	return c.UsedUSD < c.TotalLimitUSD
}

type CreateUsageCardInput struct {
	UserID           int64
	PlanID           *int64
	Name             string
	StartsAt         time.Time
	ExpiresAt        time.Time
	TotalLimitUSD    float64
	Source           string
	SourceOrderID    *int64
	SourceRedeemCode *string
	AssignedBy       *int64
	Notes            string
}
