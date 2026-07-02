package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrUsageCardDisabled        = infraerrors.Forbidden("USAGE_CARD_DISABLED", "usage card feature is disabled")
	ErrUsageCardPaymentDisabled = infraerrors.Forbidden("USAGE_CARD_PAYMENT_DISABLED", "usage card payment is disabled")
	ErrUsageCardRedeemDisabled  = infraerrors.Forbidden("USAGE_CARD_REDEEM_DISABLED", "usage card redeem is disabled")
)

type UsageCardService struct {
	repo        UsageCardRepository
	settingRepo SettingRepository
}

func NewUsageCardService(repo UsageCardRepository, settingRepo SettingRepository) *UsageCardService {
	return &UsageCardService{repo: repo, settingRepo: settingRepo}
}

func (s *UsageCardService) IsEnabled(ctx context.Context) bool {
	return s.boolSetting(ctx, SettingKeyUsageCardEnabled, false)
}

func (s *UsageCardService) IsPaymentEnabled(ctx context.Context) bool {
	return s.IsEnabled(ctx) && s.boolSetting(ctx, SettingKeyUsageCardPaymentEnabled, false)
}

func (s *UsageCardService) IsRedeemEnabled(ctx context.Context) bool {
	return s.IsEnabled(ctx) && s.boolSetting(ctx, SettingKeyUsageCardRedeemEnabled, false)
}

func (s *UsageCardService) IsBillingEnabled(ctx context.Context) bool {
	return s.IsEnabled(ctx) && s.boolSetting(ctx, SettingKeyUsageCardBillingEnabled, false)
}

func (s *UsageCardService) DefaultPriority(ctx context.Context) string {
	return BillingPriorityUsageCardFirst
}

func (s *UsageCardService) GetPlanForSale(ctx context.Context, planID int64) (*UsageCardPlan, error) {
	plan, err := s.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if !plan.ForSale {
		return nil, ErrUsageCardPlanNotFound
	}
	return plan, nil
}

func (s *UsageCardService) ListPlansForSale(ctx context.Context) ([]UsageCardPlan, error) {
	if !s.IsPaymentEnabled(ctx) {
		return []UsageCardPlan{}, nil
	}
	if s == nil || s.repo == nil {
		return []UsageCardPlan{}, nil
	}
	return s.repo.ListPlans(ctx, false)
}

func (s *UsageCardService) ListPlans(ctx context.Context, includeHidden bool) ([]UsageCardPlan, error) {
	if s == nil || s.repo == nil {
		return []UsageCardPlan{}, nil
	}
	return s.repo.ListPlans(ctx, includeHidden)
}

func (s *UsageCardService) CreatePlan(ctx context.Context, plan UsageCardPlan) (*UsageCardPlan, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageCardPlanNotFound
	}
	plan.ProductName = strings.TrimSpace(plan.ProductName)
	if strings.TrimSpace(plan.Name) == "" || plan.Price <= 0 || plan.AmountUSD <= 0 || plan.ValidityDays <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USAGE_CARD_PLAN", "invalid usage card plan")
	}
	if len([]rune(plan.ProductName)) > 100 {
		return nil, infraerrors.BadRequest("INVALID_USAGE_CARD_PLAN", "invalid usage card plan")
	}
	return s.repo.CreatePlan(ctx, plan)
}

func (s *UsageCardService) UpdatePlan(ctx context.Context, plan UsageCardPlan) (*UsageCardPlan, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageCardPlanNotFound
	}
	plan.ProductName = strings.TrimSpace(plan.ProductName)
	if plan.ID <= 0 || strings.TrimSpace(plan.Name) == "" || plan.Price <= 0 || plan.AmountUSD <= 0 || plan.ValidityDays <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USAGE_CARD_PLAN", "invalid usage card plan")
	}
	if len([]rune(plan.ProductName)) > 100 {
		return nil, infraerrors.BadRequest("INVALID_USAGE_CARD_PLAN", "invalid usage card plan")
	}
	return s.repo.UpdatePlan(ctx, plan)
}

func (s *UsageCardService) DeletePlan(ctx context.Context, id int64) error {
	if s == nil || s.repo == nil {
		return ErrUsageCardPlanNotFound
	}
	return s.repo.DeletePlan(ctx, id)
}

func (s *UsageCardService) GetPlanByID(ctx context.Context, planID int64) (*UsageCardPlan, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageCardPlanNotFound
	}
	plan, err := s.repo.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *UsageCardService) IssueFromRedeem(ctx context.Context, userID int64, code string, amountUSD float64, validityDays int, notes string) (*UserUsageCard, error) {
	if !s.IsRedeemEnabled(ctx) {
		return nil, ErrUsageCardRedeemDisabled
	}
	return s.issue(ctx, CreateUsageCardInput{
		UserID:           userID,
		Name:             "余额卡",
		TotalLimitUSD:    amountUSD,
		Source:           UsageCardSourceRedeem,
		SourceRedeemCode: usageCardStringPtr(strings.TrimSpace(code)),
		Notes:            notes,
	}, validityDays)
}

func (s *UsageCardService) IssueFromRedeemPlan(ctx context.Context, userID int64, code string, plan *UsageCardPlan, notes string) (*UserUsageCard, error) {
	if !s.IsRedeemEnabled(ctx) {
		return nil, ErrUsageCardRedeemDisabled
	}
	if plan == nil {
		return nil, ErrUsageCardPlanNotFound
	}
	return s.issue(ctx, CreateUsageCardInput{
		UserID:           userID,
		PlanID:           &plan.ID,
		Name:             plan.Name,
		TotalLimitUSD:    plan.AmountUSD,
		Source:           UsageCardSourceRedeem,
		SourceRedeemCode: usageCardStringPtr(strings.TrimSpace(code)),
		Notes:            notes,
	}, plan.ValidityDays)
}

func (s *UsageCardService) IssueFromPayment(ctx context.Context, userID int64, plan *UsageCardPlan, orderID int64, rechargeCode string) (*UserUsageCard, error) {
	if plan == nil {
		return nil, ErrUsageCardPlanNotFound
	}
	if existing, err := s.repo.GetCardBySourceOrderID(ctx, orderID); err == nil && existing != nil {
		return existing, nil
	} else if err != nil && !errors.Is(err, ErrUsageCardNotFound) {
		return nil, err
	}
	return s.issue(ctx, CreateUsageCardInput{
		UserID:           userID,
		PlanID:           &plan.ID,
		Name:             plan.Name,
		TotalLimitUSD:    plan.AmountUSD,
		Source:           UsageCardSourcePayment,
		SourceOrderID:    &orderID,
		SourceRedeemCode: usageCardStringPtr(strings.TrimSpace(rechargeCode)),
		Notes:            fmt.Sprintf("payment order %d", orderID),
	}, plan.ValidityDays)
}

func (s *UsageCardService) HasAvailableCard(ctx context.Context, userID int64, now time.Time) (bool, error) {
	if s == nil || s.repo == nil {
		return false, nil
	}
	cards, err := s.repo.ListAvailableCards(ctx, userID, now)
	if err != nil {
		return false, err
	}
	return len(cards) > 0, nil
}

func (s *UsageCardService) GetMySummary(ctx context.Context, userID int64, now time.Time) (*UsageCardSummary, error) {
	if !s.IsEnabled(ctx) {
		return &UsageCardSummary{}, nil
	}
	if s == nil || s.repo == nil {
		return &UsageCardSummary{}, nil
	}
	cards, err := s.repo.ListAvailableCards(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	summary := &UsageCardSummary{AvailableCount: len(cards)}
	for i := range cards {
		remaining := cards[i].RemainingUSD()
		if remaining > 0 {
			summary.AvailableRemainingUSD += remaining
		}
	}
	return summary, nil
}

func (s *UsageCardService) ListMyCards(ctx context.Context, userID int64) ([]UserUsageCard, error) {
	if !s.IsEnabled(ctx) {
		return []UserUsageCard{}, nil
	}
	if s == nil || s.repo == nil {
		return []UserUsageCard{}, nil
	}
	return s.repo.ListUserCards(ctx, userID, false)
}

func (s *UsageCardService) ListCards(ctx context.Context, userID *int64, status string) ([]UserUsageCard, error) {
	if s == nil || s.repo == nil {
		return []UserUsageCard{}, nil
	}
	return s.repo.ListCards(ctx, userID, strings.TrimSpace(status))
}

func (s *UsageCardService) DeductFirstAvailable(ctx context.Context, userID int64, amount float64, now time.Time) (*UserUsageCard, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageCardUnavailable
	}
	cards, err := s.repo.ListAvailableCards(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		card, err := s.repo.DeductCard(ctx, cards[i].ID, userID, amount, now)
		if err == nil {
			return card, nil
		}
	}
	return nil, ErrUsageCardUnavailable
}

func (s *UsageCardService) CancelCard(ctx context.Context, cardID int64, operatorID int64, reason string) error {
	if s == nil || s.repo == nil {
		return ErrUsageCardNotFound
	}
	return s.repo.UpdateCardStatus(ctx, cardID, UsageCardStatusCancelled, reason, operatorID)
}

func (s *UsageCardService) SuspendCard(ctx context.Context, cardID int64, operatorID int64, reason string) error {
	if s == nil || s.repo == nil {
		return ErrUsageCardNotFound
	}
	return s.repo.UpdateCardStatus(ctx, cardID, UsageCardStatusSuspended, reason, operatorID)
}

func (s *UsageCardService) ResumeCard(ctx context.Context, cardID int64, operatorID int64, reason string) error {
	if s == nil || s.repo == nil {
		return ErrUsageCardNotFound
	}
	return s.repo.UpdateCardStatus(ctx, cardID, UsageCardStatusActive, reason, operatorID)
}

func (s *UsageCardService) issue(ctx context.Context, input CreateUsageCardInput, validityDays int) (*UserUsageCard, error) {
	if s == nil || s.repo == nil {
		return nil, ErrUsageCardDisabled
	}
	if input.UserID <= 0 || input.TotalLimitUSD <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USAGE_CARD", "invalid usage card input")
	}
	if validityDays <= 0 {
		validityDays = 30
	}
	now := time.Now()
	input.StartsAt = now
	input.ExpiresAt = now.AddDate(0, 0, validityDays)
	if strings.TrimSpace(input.Source) == "" {
		input.Source = UsageCardSourceAdmin
	}
	return s.repo.CreateCard(ctx, input)
}

func (s *UsageCardService) boolSetting(ctx context.Context, key string, fallback bool) bool {
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	v, err := s.settingRepo.GetValue(ctx, key)
	if err != nil {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func (s *UsageCardService) stringSetting(ctx context.Context, key string, fallback string) string {
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	v, err := s.settingRepo.GetValue(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func usageCardStringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
