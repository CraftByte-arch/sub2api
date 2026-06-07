package service

import (
	"context"
	"time"
)

type UsageCardRepository interface {
	ListPlans(ctx context.Context, includeHidden bool) ([]UsageCardPlan, error)
	CreateCard(ctx context.Context, input CreateUsageCardInput) (*UserUsageCard, error)
	CreatePlan(ctx context.Context, plan UsageCardPlan) (*UsageCardPlan, error)
	GetCardBySourceOrderID(ctx context.Context, orderID int64) (*UserUsageCard, error)
	GetPlanByID(ctx context.Context, id int64) (*UsageCardPlan, error)
	UpdatePlan(ctx context.Context, plan UsageCardPlan) (*UsageCardPlan, error)
	DeletePlan(ctx context.Context, id int64) error
	ListUserCards(ctx context.Context, userID int64, includeDeleted bool) ([]UserUsageCard, error)
	ListCards(ctx context.Context, userID *int64, status string) ([]UserUsageCard, error)
	ListAvailableCards(ctx context.Context, userID int64, now time.Time) ([]UserUsageCard, error)
	DeductCard(ctx context.Context, cardID, userID int64, amount float64, now time.Time) (*UserUsageCard, error)
	UpdateCardStatus(ctx context.Context, cardID int64, status string, reason string, operatorID int64) error
}
