package admin

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UsageCardHandler struct {
	usageCardService *service.UsageCardService
}

func NewUsageCardHandler(usageCardService *service.UsageCardService) *UsageCardHandler {
	return &UsageCardHandler{usageCardService: usageCardService}
}

type usageCardPlanRequest struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Price        float64 `json:"price"`
	AmountUSD    float64 `json:"amount_usd"`
	ValidityDays int     `json:"validity_days"`
	Features     string  `json:"features"`
	ForSale      bool    `json:"for_sale"`
	SortOrder    int     `json:"sort_order"`
}

type usageCardStatusRequest struct {
	Reason string `json:"reason"`
}

type usageCardPlanResponse struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Price        float64 `json:"price"`
	AmountUSD    float64 `json:"amount_usd"`
	ValidityDays int     `json:"validity_days"`
	Features     string  `json:"features"`
	ForSale      bool    `json:"for_sale"`
	SortOrder    int     `json:"sort_order"`
}

type usageCardResponse struct {
	ID               int64                  `json:"id"`
	UserID           int64                  `json:"user_id"`
	User             *usageCardUserResponse `json:"user,omitempty"`
	PlanID           *int64                 `json:"plan_id,omitempty"`
	Name             string                 `json:"name"`
	StartsAt         time.Time              `json:"starts_at"`
	ExpiresAt        time.Time              `json:"expires_at"`
	TotalLimitUSD    float64                `json:"total_limit_usd"`
	UsedUSD          float64                `json:"used_usd"`
	RemainingUSD     float64                `json:"remaining_usd"`
	Status           string                 `json:"status"`
	Source           string                 `json:"source"`
	SourceOrderID    *int64                 `json:"source_order_id,omitempty"`
	SourceRedeemCode *string                `json:"source_redeem_code,omitempty"`
	AssignedBy       *int64                 `json:"assigned_by,omitempty"`
	Notes            string                 `json:"notes,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	DeletedAt        *time.Time             `json:"deleted_at,omitempty"`
}

type usageCardUserResponse struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

func (h *UsageCardHandler) ListPlans(c *gin.Context) {
	plans, err := h.usageCardService.ListPlans(c.Request.Context(), true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usageCardPlanResponses(plans))
}

func (h *UsageCardHandler) CreatePlan(c *gin.Context) {
	var req usageCardPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	plan, err := h.usageCardService.CreatePlan(c.Request.Context(), service.UsageCardPlan{
		Name:         req.Name,
		Description:  req.Description,
		Price:        req.Price,
		AmountUSD:    req.AmountUSD,
		ValidityDays: req.ValidityDays,
		Features:     req.Features,
		ForSale:      req.ForSale,
		SortOrder:    req.SortOrder,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usageCardPlanResponseFromService(plan))
}

func (h *UsageCardHandler) UpdatePlan(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req usageCardPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	plan, err := h.usageCardService.UpdatePlan(c.Request.Context(), service.UsageCardPlan{
		ID:           id,
		Name:         req.Name,
		Description:  req.Description,
		Price:        req.Price,
		AmountUSD:    req.AmountUSD,
		ValidityDays: req.ValidityDays,
		Features:     req.Features,
		ForSale:      req.ForSale,
		SortOrder:    req.SortOrder,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usageCardPlanResponseFromService(plan))
}

func (h *UsageCardHandler) DeletePlan(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.usageCardService.DeletePlan(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *UsageCardHandler) ListCards(c *gin.Context) {
	var userID *int64
	if raw := c.Query("user_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		userID = &id
	}
	cards, err := h.usageCardService.ListCards(c.Request.Context(), userID, c.Query("status"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usageCardResponses(cards))
}

func (h *UsageCardHandler) CancelCard(c *gin.Context) {
	h.updateCardStatus(c, "cancel")
}

func (h *UsageCardHandler) SuspendCard(c *gin.Context) {
	h.updateCardStatus(c, "suspend")
}

func (h *UsageCardHandler) ResumeCard(c *gin.Context) {
	h.updateCardStatus(c, "resume")
}

func (h *UsageCardHandler) updateCardStatus(c *gin.Context, action string) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req usageCardStatusRequest
	_ = c.ShouldBindJSON(&req)
	operatorID := getAdminIDFromContext(c)
	var err error
	switch action {
	case "cancel":
		err = h.usageCardService.CancelCard(c.Request.Context(), id, operatorID, req.Reason)
	case "suspend":
		err = h.usageCardService.SuspendCard(c.Request.Context(), id, operatorID, req.Reason)
	default:
		err = h.usageCardService.ResumeCard(c.Request.Context(), id, operatorID, req.Reason)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"updated": true})
}

func usageCardPlanResponses(plans []service.UsageCardPlan) []usageCardPlanResponse {
	out := make([]usageCardPlanResponse, 0, len(plans))
	for i := range plans {
		out = append(out, usageCardPlanResponseFromService(&plans[i]))
	}
	return out
}

func usageCardPlanResponseFromService(plan *service.UsageCardPlan) usageCardPlanResponse {
	return usageCardPlanResponse{
		ID:           plan.ID,
		Name:         plan.Name,
		Description:  plan.Description,
		Price:        plan.Price,
		AmountUSD:    plan.AmountUSD,
		ValidityDays: plan.ValidityDays,
		Features:     plan.Features,
		ForSale:      plan.ForSale,
		SortOrder:    plan.SortOrder,
	}
}

func usageCardResponses(cards []service.UserUsageCard) []usageCardResponse {
	out := make([]usageCardResponse, 0, len(cards))
	for i := range cards {
		out = append(out, usageCardResponseFromService(&cards[i]))
	}
	return out
}

func usageCardResponseFromService(card *service.UserUsageCard) usageCardResponse {
	resp := usageCardResponse{
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
	if card.User != nil {
		resp.User = &usageCardUserResponse{
			ID:       card.User.ID,
			Email:    card.User.Email,
			Username: card.User.Username,
		}
	}
	return resp
}
