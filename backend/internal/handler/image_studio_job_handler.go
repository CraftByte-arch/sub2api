package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	imageStudioMultipartMaxFileBytes   = int64(20 << 20)
	imageStudioMultipartMaxScalarBytes = int64(64 << 10)
	imageStudioMultipartMaxBodyBytes   = 5*imageStudioMultipartMaxFileBytes + 1<<20
)

type ImageStudioJobHandler struct {
	jobService                   *service.ImageStudioJobService
	apiKeyService                *service.APIKeyService
	createMultipartTempFile      func() (imageStudioMultipartTempFile, error)
	observeMultipartCleanupError func(error)
}

type imageStudioMultipartTempFile interface {
	io.ReadWriteSeeker
	io.Closer
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
	ResponseFormat    string   `json:"response_format"`
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
		jobService:                   jobService,
		apiKeyService:                apiKeyService,
		createMultipartTempFile:      newImageStudioMultipartTempFile,
		observeMultipartCleanupError: func(err error) { log.Printf("image studio multipart cleanup failed: %v", err) },
	}
}

func (h *ImageStudioJobHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		response.BadRequest(c, "Invalid Content-Type")
		return
	}
	var req imageStudioCreateJobRequest
	var images []service.UploadedFile
	var mask *service.UploadedFile
	var cleanup func() error
	switch mediaType {
	case "application/json":
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return
		}
		mode := strings.TrimSpace(req.Mode)
		if mode == "" {
			mode = service.ImageStudioJobModeGenerate
		}
		if mode == service.ImageStudioJobModeEdit {
			response.BadRequest(c, "JSON edit jobs are no longer accepted; use multipart/form-data with repeated image fields instead of image_data_urls or mask_data_url")
			return
		}
		if mode != service.ImageStudioJobModeGenerate {
			response.BadRequest(c, "application/json only supports generate mode")
			return
		}
		req.Mode = mode
	case "multipart/form-data":
		var parseErr error
		req, images, mask, cleanup, parseErr = parseImageStudioEditMultipart(c, h.createMultipartTempFile)
		if parseErr != nil {
			response.ErrorFrom(c, parseErr)
			return
		}
		defer func() {
			if cleanupErr := cleanup(); cleanupErr != nil && h.observeMultipartCleanupError != nil {
				h.observeMultipartCleanupError(cleanupErr)
			}
		}()
	default:
		response.BadRequest(c, "Content-Type must be application/json for generation or multipart/form-data for edits")
		return
	}
	mode := strings.TrimSpace(req.Mode)

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

	createInput := service.ImageStudioJobCreateInput{
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
	}
	var job *service.ImageStudioJob
	if mode == service.ImageStudioJobModeEdit {
		job, err = h.jobService.CreateEditJob(c.Request.Context(), createInput, images, mask)
	} else {
		job, err = h.jobService.CreateJob(c.Request.Context(), createInput)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, h.toJobResponse(job))
}

func parseImageStudioEditMultipart(c *gin.Context, createTempFile func() (imageStudioMultipartTempFile, error)) (_ imageStudioCreateJobRequest, _ []service.UploadedFile, _ *service.UploadedFile, cleanup func() error, retErr error) {
	var req imageStudioCreateJobRequest
	tempFiles := make([]imageStudioMultipartTempFile, 0, 5)
	cleanup = func() error {
		errs := make([]error, 0, len(tempFiles))
		for _, file := range tempFiles {
			if file == nil {
				continue
			}
			errs = append(errs, file.Close())
		}
		return errors.Join(errs...)
	}
	defer func() {
		if retErr != nil {
			if cleanupErr := cleanup(); cleanupErr != nil {
				retErr = multipartStorageError(errors.Join(retErr, cleanupErr))
			}
		}
	}()

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, imageStudioMultipartMaxBodyBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return req, nil, nil, cleanup, multipartValidationError("invalid multipart request", err)
	}
	seenScalars := make(map[string]struct{})
	images := make([]service.UploadedFile, 0, 4)
	var mask *service.UploadedFile
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return req, nil, nil, cleanup, multipartValidationError("invalid or oversized multipart request", nextErr)
		}
		name := strings.TrimSpace(part.FormName())
		switch name {
		case "image", "mask":
			if name == "image" && len(images) >= 4 {
				return req, nil, nil, cleanup, multipartValidationError("edit jobs accept at most four image fields", nil)
			}
			if name == "mask" && mask != nil {
				return req, nil, nil, cleanup, multipartValidationError("mask must appear at most once", nil)
			}
			tempFile, createErr := createTempFile()
			if createErr != nil {
				return req, nil, nil, cleanup, multipartStorageError(createErr)
			}
			tempFiles = append(tempFiles, tempFile)
			copyErr := copyImageStudioMultipartFile(tempFile, part)
			if copyErr != nil {
				return req, nil, nil, cleanup, copyErr
			}
			if _, seekErr := tempFile.Seek(0, io.SeekStart); seekErr != nil {
				return req, nil, nil, cleanup, multipartStorageError(seekErr)
			}
			upload := service.UploadedFile{Reader: tempFile, ContentType: part.Header.Get("Content-Type")}
			if name == "image" {
				images = append(images, upload)
			} else {
				mask = &upload
			}
		default:
			if _, exists := seenScalars[name]; exists {
				return req, nil, nil, cleanup, multipartValidationError(name+" must appear at most once", nil)
			}
			seenScalars[name] = struct{}{}
			value, readErr := io.ReadAll(io.LimitReader(part, imageStudioMultipartMaxScalarBytes+1))
			if readErr != nil {
				return req, nil, nil, cleanup, multipartValidationError("failed to read multipart field", readErr)
			}
			if int64(len(value)) > imageStudioMultipartMaxScalarBytes {
				return req, nil, nil, cleanup, multipartValidationError(name+" exceeds the scalar size limit", nil)
			}
			if fieldErr := setImageStudioMultipartScalar(&req, name, string(value)); fieldErr != nil {
				return req, nil, nil, cleanup, fieldErr
			}
		}
	}
	if len(images) == 0 {
		return req, nil, nil, cleanup, multipartValidationError("edit jobs require at least one image field", nil)
	}
	if strings.TrimSpace(req.Mode) != service.ImageStudioJobModeEdit {
		return req, nil, nil, cleanup, multipartValidationError("multipart/form-data only supports edit mode", nil)
	}
	if req.APIKeyID <= 0 {
		return req, nil, nil, cleanup, multipartValidationError("api_key_id is required", nil)
	}
	return req, images, mask, cleanup, nil
}

func newImageStudioMultipartTempFile() (imageStudioMultipartTempFile, error) {
	file, err := os.CreateTemp("", "image-studio-upload-*")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(file.Name()); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func copyImageStudioMultipartFile(dst io.Writer, src io.Reader) error {
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		read, readErr := src.Read(buffer)
		if read > 0 {
			total += int64(read)
			if total > imageStudioMultipartMaxFileBytes {
				return multipartValidationError("multipart file exceeds the per-file size limit", nil)
			}
			written, writeErr := dst.Write(buffer[:read])
			if writeErr != nil {
				return multipartStorageError(writeErr)
			}
			if written != read {
				return multipartStorageError(io.ErrShortWrite)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return multipartValidationError("failed to read multipart file", readErr)
		}
	}
}

func setImageStudioMultipartScalar(req *imageStudioCreateJobRequest, name, value string) error {
	switch name {
	case "api_key_id":
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || parsed <= 0 {
			return multipartValidationError("api_key_id must be a positive integer", err)
		}
		req.APIKeyID = parsed
	case "mode":
		req.Mode = value
	case "prompt":
		req.Prompt = value
	case "model":
		req.Model = value
	case "size":
		req.Size = value
	case "output_format":
		req.OutputFormat = value
	case "quality":
		req.Quality = value
	case "background":
		req.Background = value
	case "style":
		req.Style = value
	case "moderation":
		req.Moderation = value
	case "input_fidelity":
		req.InputFidelity = value
	case "response_format":
		req.ResponseFormat = value
	case "output_compression":
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return multipartValidationError("output_compression must be an integer", err)
		}
		req.OutputCompression = &parsed
	default:
		return multipartValidationError("unknown multipart field: "+name, nil)
	}
	return nil
}

func multipartValidationError(message string, cause error) error {
	err := infraerrors.BadRequest("IMAGE_STUDIO_MULTIPART_INVALID", message)
	if cause != nil {
		return err.WithCause(cause)
	}
	return err
}

func multipartStorageError(cause error) error {
	return infraerrors.ServiceUnavailable(
		"IMAGE_STUDIO_INPUT_STORAGE_UNAVAILABLE",
		"image studio input storage is unavailable",
	).WithCause(cause)
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
	responseFormat := "b64_json"
	if mode == service.ImageStudioJobModeEdit {
		responseFormat = firstNonEmptyImageStudio(req.ResponseFormat, responseFormat)
	}
	payload := map[string]any{
		"model":           model,
		"prompt":          strings.TrimSpace(req.Prompt),
		"response_format": responseFormat,
		"output_format":   outputFormat,
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
