package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UsageCardHandler struct {
	usageCardService *service.UsageCardService
}

func NewUsageCardHandler(usageCardService *service.UsageCardService) *UsageCardHandler {
	return &UsageCardHandler{usageCardService: usageCardService}
}

type usageCardResponse struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"user_id"`
	PlanID           *int64     `json:"plan_id,omitempty"`
	Name             string     `json:"name"`
	StartsAt         time.Time  `json:"starts_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	TotalLimitUSD    float64    `json:"total_limit_usd"`
	UsedUSD          float64    `json:"used_usd"`
	RemainingUSD     float64    `json:"remaining_usd"`
	Status           string     `json:"status"`
	Source           string     `json:"source"`
	SourceOrderID    *int64     `json:"source_order_id,omitempty"`
	SourceRedeemCode *string    `json:"source_redeem_code,omitempty"`
	AssignedBy       *int64     `json:"assigned_by,omitempty"`
	Notes            string     `json:"notes,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

func (h *UsageCardHandler) ListMyCards(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	cards, err := h.usageCardService.ListMyCards(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usageCardResponses(cards))
}

func usageCardResponses(cards []service.UserUsageCard) []usageCardResponse {
	out := make([]usageCardResponse, 0, len(cards))
	for i := range cards {
		out = append(out, usageCardResponseFromService(&cards[i]))
	}
	return out
}

func usageCardResponseFromService(card *service.UserUsageCard) usageCardResponse {
	return usageCardResponse{
		ID:               card.ID,
		UserID:           card.UserID,
		PlanID:           card.PlanID,
		Name:             card.Name,
		StartsAt:         card.StartsAt,
		ExpiresAt:        card.ExpiresAt,
		TotalLimitUSD:    card.TotalLimitUSD,
		UsedUSD:          card.UsedUSD,
		RemainingUSD:     card.RemainingUSD(),
		Status:           card.Status,
		Source:           card.Source,
		SourceOrderID:    card.SourceOrderID,
		SourceRedeemCode: card.SourceRedeemCode,
		AssignedBy:       card.AssignedBy,
		Notes:            card.Notes,
		CreatedAt:        card.CreatedAt,
		UpdatedAt:        card.UpdatedAt,
		DeletedAt:        card.DeletedAt,
	}
}
