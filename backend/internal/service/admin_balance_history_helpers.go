package service

import (
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func excludeRedeemCodesByCode(codes []RedeemCode, blocked map[string]struct{}) []RedeemCode {
	if len(codes) == 0 || len(blocked) == 0 {
		return codes
	}
	filtered := make([]RedeemCode, 0, len(codes))
	for _, code := range codes {
		if code.Type == RedeemTypeBalance {
			if _, ok := blocked[strings.TrimSpace(code.Code)]; ok {
				continue
			}
		}
		filtered = append(filtered, code)
	}
	return filtered
}

func usageCardPurchaseHistoryFromOrder(order *dbent.PaymentOrder, plans map[int64]*UsageCardPlan) RedeemCode {
	if order == nil {
		return RedeemCode{}
	}
	usedBy := order.UserID
	usedAt := order.CreatedAt
	if order.PaidAt != nil {
		usedAt = *order.PaidAt
	}
	if order.CompletedAt != nil {
		usedAt = *order.CompletedAt
	}
	var plan *UsageCardPlan
	var planID *int64
	value := order.Amount
	if order.PlanID != nil {
		id := *order.PlanID
		planID = &id
		if p := plans[id]; p != nil {
			plan = p
			if p.AmountUSD > 0 {
				value = p.AmountUSD
			}
		}
	}
	return RedeemCode{
		ID:              -order.ID,
		Code:            order.OutTradeNo,
		Type:            RedeemTypeUsageCard,
		Source:          "purchase",
		OrderType:       payment.OrderTypeUsageCard,
		Value:           value,
		Status:          StatusUsed,
		UsedBy:          &usedBy,
		UsedAt:          &usedAt,
		Notes:           "usage_card_purchase",
		CreatedAt:       order.CreatedAt,
		UsageCardPlanID: planID,
		UsageCardPlan:   plan,
	}
}

func paymentOrderHistoryFromOrder(order *dbent.PaymentOrder, plans map[int64]*UsageCardPlan) RedeemCode {
	if order == nil {
		return RedeemCode{}
	}
	usedBy := order.UserID
	usedAt := order.CreatedAt
	if order.PaidAt != nil {
		usedAt = *order.PaidAt
	}
	if order.CompletedAt != nil {
		usedAt = *order.CompletedAt
	}
	item := RedeemCode{
		ID:        -order.ID,
		Code:      order.OutTradeNo,
		Source:    "purchase",
		OrderType: order.OrderType,
		Status:    StatusUsed,
		UsedBy:    &usedBy,
		UsedAt:    &usedAt,
		CreatedAt: order.CreatedAt,
	}
	switch order.OrderType {
	case payment.OrderTypeBalance:
		item.Type = RedeemTypeBalance
		item.Value = order.Amount
		item.Notes = "balance_purchase"
	case payment.OrderTypeSubscription:
		item.Type = RedeemTypeSubscription
		if order.SubscriptionDays != nil {
			item.Value = float64(*order.SubscriptionDays)
			item.ValidityDays = *order.SubscriptionDays
		}
		if order.SubscriptionGroupID != nil {
			id := *order.SubscriptionGroupID
			item.GroupID = &id
		}
		item.Notes = "subscription_purchase"
	case payment.OrderTypeUsageCard:
		return usageCardPurchaseHistoryFromOrder(order, plans)
	}
	return item
}
