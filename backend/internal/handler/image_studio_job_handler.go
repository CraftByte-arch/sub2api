package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ImageStudioJobHandler struct {
	jobService    *service.ImageStudioJobService
	apiKeyService *service.APIKeyService
}

type imageStudioCreateJobRequest struct {
	APIKeyID          int64    `json:"api_key_id" binding:"required"`
	Mode              string   `json:"mode"`
	Prompt            string   `json:"prompt"`
	Model             string   `json:"model"`
	Size              string   `json:"size"`
	OutputFormat      string   `json:"output_format"`
	Quality           string   `json:"quality"`
	Background        string   `json:"background"`
	Style             string   `json:"style"`
	Moderation        string   `json:"moderation"`
	InputFidelity     string   `json:"input_fidelity"`
	OutputCompression *int     `json:"output_compression"`
	ImageDataURLs     []string `json:"image_data_urls"`
	MaskDataURL       string   `json:"mask_data_url"`
}

type imageStudioJobResponse struct {
	ID               int64      `json:"id"`
	Mode             string     `json:"mode"`
	Status           string     `json:"status"`
	Prompt           string     `json:"prompt"`
	Model            string     `json:"model"`
	Size             string     `json:"size"`
	OutputFormat     string     `json:"output_format"`
	EstimatedCostUSD float64    `json:"estimated_cost_usd"`
	ChargedAmountUSD float64    `json:"charged_amount_usd"`
	MIMEType         string     `json:"mime_type"`
	FileSizeBytes    int64      `json:"file_size_bytes"`
	Width            int        `json:"width"`
	Height           int        `json:"height"`
	ErrorCode        string     `json:"error_code"`
	ErrorMessage     string     `json:"error_message"`
	QueuedAt         time.Time  `json:"queued_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	AssetsDeletedAt  *time.Time `json:"assets_deleted_at,omitempty"`
	ThumbnailURL     string     `json:"thumbnail_url,omitempty"`
	OriginalURL      string     `json:"original_url,omitempty"`
}

type imageStudioJobStatsResponse struct {
	PendingCount int64 `json:"pending_count"`
	FailedCount  int64 `json:"failed_count"`
}

func NewImageStudioJobHandler(jobService *service.ImageStudioJobService, apiKeyService *service.APIKeyService) *ImageStudioJobHandler {
	return &ImageStudioJobHandler{
		jobService:    jobService,
		apiKeyService: apiKeyService,
	}
}

func (h *ImageStudioJobHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req imageStudioCreateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = service.ImageStudioJobModeGenerate
	}
	if mode != service.ImageStudioJobModeGenerate && mode != service.ImageStudioJobModeEdit {
		response.BadRequest(c, "mode must be generate or edit")
		return
	}
	if mode == service.ImageStudioJobModeEdit {
		if len(req.ImageDataURLs) == 0 {
			response.BadRequest(c, "image_data_urls is required for edit mode")
			return
		}
		if len(req.ImageDataURLs) > 1 {
			response.BadRequest(c, "only one input image is supported")
			return
		}
	}

	apiKey, err := h.apiKeyService.GetByID(c.Request.Context(), req.APIKeyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if apiKey.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}
	if !apiKey.IsActive() {
		response.Forbidden(c, "API key is inactive")
		return
	}
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		response.Forbidden(c, service.ImageGenerationPermissionMessage())
		return
	}
	if err := h.jobService.ValidateAPIKeyAvailableForImageStudio(c.Request.Context(), apiKey); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	payload, err := buildImageStudioJobPayload(req, mode)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	estimatedCostBreakdown, err := h.jobService.EstimateCost(c.Request.Context(), apiKey, req.Model, req.Size)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	estimatedCost := 0.0
	if estimatedCostBreakdown != nil {
		estimatedCost = estimatedCostBreakdown.ActualCost
	}
	outputFormat := strings.TrimSpace(req.OutputFormat)
	if outputFormat == "" {
		outputFormat = "png"
	}

	job, err := h.jobService.CreateJob(c.Request.Context(), service.ImageStudioJobCreateInput{
		UserID:           subject.UserID,
		APIKeyID:         apiKey.ID,
		Mode:             mode,
		Prompt:           strings.TrimSpace(req.Prompt),
		Model:            strings.TrimSpace(req.Model),
		Size:             strings.TrimSpace(req.Size),
		OutputFormat:     outputFormat,
		EstimatedCostUSD: estimatedCost,
		BillingPriority:  service.NormalizeBillingPriority(apiKey.BillingPriority),
		RequestPayload:   payload,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.toJobResponse(job))
}

func (h *ImageStudioJobHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	list, err := h.jobService.ListJobs(c.Request.Context(), subject.UserID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	items := make([]imageStudioJobResponse, 0, len(list.Items))
	for i := range list.Items {
		items = append(items, h.toJobResponse(&list.Items[i]))
	}
	response.Paginated(c, items, list.Total, page, pageSize)
}

func (h *ImageStudioJobHandler) Get(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid job ID")
		return
	}
	job, err := h.jobService.GetJob(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.toJobResponse(job))
}

func (h *ImageStudioJobHandler) Stats(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	stats, err := h.jobService.JobStats(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, imageStudioJobStatsResponse{
		PendingCount: stats.PendingCount,
		FailedCount:  stats.FailedCount,
	})
}

func (h *ImageStudioJobHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid job ID")
		return
	}
	if err := h.jobService.DeleteJob(c.Request.Context(), subject.UserID, id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *ImageStudioJobHandler) GetThumbnail(c *gin.Context) {
	h.serveAsset(c, true)
}

func (h *ImageStudioJobHandler) GetOriginal(c *gin.Context) {
	h.serveAsset(c, false)
}

func (h *ImageStudioJobHandler) serveAsset(c *gin.Context, thumbnail bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid job ID")
		return
	}
	job, err := h.jobService.GetJob(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	path := strings.TrimSpace(job.OriginalPath)
	if thumbnail {
		path = strings.TrimSpace(job.ThumbnailPath)
	}
	if path == "" || job.AssetsDeletedAt != nil {
		response.NotFound(c, "Image asset not found")
		return
	}
	if _, err := os.Stat(path); err != nil {
		response.NotFound(c, "Image asset not found")
		return
	}

	if thumbnail {
		c.Header("Cache-Control", "private, max-age=300")
	} else {
		c.Header("Cache-Control", "private, max-age=31536000, immutable")
	}
	c.File(path)
}

func (h *ImageStudioJobHandler) toJobResponse(job *service.ImageStudioJob) imageStudioJobResponse {
	if job == nil {
		return imageStudioJobResponse{}
	}
	result := imageStudioJobResponse{
		ID:               job.ID,
		Mode:             job.Mode,
		Status:           job.Status,
		Prompt:           job.Prompt,
		Model:            job.Model,
		Size:             job.Size,
		OutputFormat:     job.OutputFormat,
		EstimatedCostUSD: job.EstimatedCostUSD,
		ChargedAmountUSD: job.ChargedAmountUSD,
		MIMEType:         job.MIMEType,
		FileSizeBytes:    job.FileSizeBytes,
		Width:            job.Width,
		Height:           job.Height,
		ErrorCode:        job.ErrorCode,
		ErrorMessage:     job.ErrorMessage,
		QueuedAt:         job.QueuedAt,
		StartedAt:        job.StartedAt,
		CompletedAt:      job.CompletedAt,
		ExpiresAt:        job.ExpiresAt,
		AssetsDeletedAt:  job.AssetsDeletedAt,
	}
	if job.Status == service.ImageStudioJobStatusSucceeded && job.AssetsDeletedAt == nil {
		if thumbnailPath := strings.TrimSpace(job.ThumbnailPath); thumbnailPath != "" {
			result.ThumbnailURL = fmt.Sprintf("/api/v1/image-studio/jobs/%d/thumbnail", job.ID)
		}
		if strings.TrimSpace(job.OriginalPath) != "" {
			result.OriginalURL = fmt.Sprintf("/api/v1/image-studio/jobs/%d/original", job.ID)
		}
	}
	return result
}

func buildImageStudioJobPayload(req imageStudioCreateJobRequest, mode string) (json.RawMessage, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	outputFormat := firstNonEmptyImageStudio(req.OutputFormat, "png")
	payload := map[string]any{
		"model":           model,
		"prompt":          strings.TrimSpace(req.Prompt),
		"response_format": "b64_json",
		"output_format":   outputFormat,
	}
	if mode == service.ImageStudioJobModeEdit {
		payload["images"] = []map[string]any{{
			"image_url": strings.TrimSpace(req.ImageDataURLs[0]),
		}}
		if maskDataURL := strings.TrimSpace(req.MaskDataURL); maskDataURL != "" {
			payload["mask"] = map[string]any{"image_url": maskDataURL}
		}
	}
	if size := strings.TrimSpace(req.Size); size != "" {
		payload["size"] = size
	}
	if quality := strings.TrimSpace(req.Quality); quality != "" {
		payload["quality"] = quality
	}
	if background := strings.TrimSpace(req.Background); background != "" {
		payload["background"] = background
	}
	if style := strings.TrimSpace(req.Style); style != "" {
		payload["style"] = style
	}
	if moderation := strings.TrimSpace(req.Moderation); moderation != "" {
		payload["moderation"] = moderation
	}
	if inputFidelity := strings.TrimSpace(req.InputFidelity); inputFidelity != "" {
		payload["input_fidelity"] = inputFidelity
	}
	if req.OutputCompression != nil {
		payload["output_compression"] = *req.OutputCompression
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func firstNonEmptyImageStudio(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
