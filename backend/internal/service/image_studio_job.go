package service

import (
	"context"
	"encoding/json"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ImageStudioJobModeGenerate = "generate"
	ImageStudioJobModeEdit     = "edit"

	ImageStudioJobStatusQueued    = "queued"
	ImageStudioJobStatusRunning   = "running"
	ImageStudioJobStatusSettling  = "settling"
	ImageStudioJobStatusSucceeded = "succeeded"
	ImageStudioJobStatusFailed    = "failed"
)

const (
	ImageStudioRetentionUnitHour = "hour"
	ImageStudioRetentionUnitDay  = "day"
)

var (
	ErrImageStudioJobNotFound       = infraerrors.NotFound("IMAGE_STUDIO_JOB_NOT_FOUND", "image studio job not found")
	ErrImageStudioJobInvalid        = infraerrors.BadRequest("IMAGE_STUDIO_JOB_INVALID", "image studio job payload is invalid")
	ErrImageStudioGroupNotAvailable = infraerrors.Forbidden("IMAGE_STUDIO_GROUP_NOT_AVAILABLE", "API key group is not available for image studio")
)

type ImageStudioJob struct {
	ID                     int64
	UserID                 int64
	APIKeyID               int64
	Mode                   string
	Status                 string
	RequestPayload         json.RawMessage
	SettlementPayload      json.RawMessage
	Prompt                 string
	Model                  string
	Size                   string
	OutputFormat           string
	EstimatedCostUSD       float64
	ChargedAmountUSD       float64
	BillingPriority        string
	HoldBalanceAmountUSD   float64
	HoldUsageCardAmountUSD float64
	HoldUsageCardID        *int64
	OriginalPath           string
	ThumbnailPath          string
	MIMEType               string
	FileSizeBytes          int64
	Width                  int
	Height                 int
	ErrorCode              string
	ErrorMessage           string
	AttemptCount           int
	MaxAttempts            int
	NextAttemptAt          *time.Time
	QueuedAt               time.Time
	StartedAt              *time.Time
	HeartbeatAt            *time.Time
	CompletedAt            *time.Time
	ExpiresAt              *time.Time
	AssetsDeletedAt        *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ImageStudioJobList struct {
	Items []ImageStudioJob
	Total int64
}

type ImageStudioJobStats struct {
	PendingCount int64
	FailedCount  int64
}

type ImageStudioJobCreateInput struct {
	UserID           int64
	APIKeyID         int64
	Mode             string
	Prompt           string
	Model            string
	Size             string
	OutputFormat     string
	EstimatedCostUSD float64
	BillingPriority  string
	RequestPayload   json.RawMessage
}

type ImageStudioJobRepository interface {
	Create(ctx context.Context, input ImageStudioJobCreateInput) (*ImageStudioJob, error)
	GetByID(ctx context.Context, id int64) (*ImageStudioJob, error)
	GetByIDForUser(ctx context.Context, id, userID int64) (*ImageStudioJob, error)
	ListByUser(ctx context.Context, userID int64, page, pageSize int) (*ImageStudioJobList, error)
	CountStatusByUser(ctx context.Context, userID int64) (*ImageStudioJobStats, error)
	DeleteByIDForUser(ctx context.Context, id, userID int64) error
	ListRunnableJobs(ctx context.Context, limit int) ([]ImageStudioJob, error)
	MarkRunning(ctx context.Context, id int64, startedAt time.Time) (bool, error)
	MarkStaleRunningFailed(ctx context.Context, id int64, completedAt, staleBefore time.Time) (bool, error)
	MarkSettling(ctx context.Context, id int64, settlementPayload json.RawMessage, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, leaseAt time.Time) error
	ClaimSettling(ctx context.Context, id int64, leaseAt, staleBefore time.Time) (bool, error)
	UpdateHeartbeat(ctx context.Context, id int64, heartbeatAt time.Time) error
	MarkRetryable(ctx context.Context, id int64, nextAttemptAt time.Time, errorCode, errorMessage string) error
	MarkSettlementRetryable(ctx context.Context, id int64, nextAttemptAt time.Time, errorCode, errorMessage string) error
	MarkSucceeded(ctx context.Context, id int64, completedAt time.Time, chargedAmountUSD float64, originalPath, thumbnailPath, mimeType string, fileSizeBytes int64, width, height int, expiresAt *time.Time) error
	MarkFailed(ctx context.Context, id int64, completedAt time.Time, errorCode, errorMessage string) error
	ListExpiredAssets(ctx context.Context, now time.Time, limit int) ([]ImageStudioJob, error)
	MarkAssetsDeleted(ctx context.Context, id int64, deletedAt time.Time) error
}
