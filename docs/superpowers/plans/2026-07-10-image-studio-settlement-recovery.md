# Image Studio Settlement Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Image Studio generation and settlement converge after billing retries, worker interruption, dependency deletion, and asset-persistence failures without replaying uncertain upstream requests.

**Architecture:** Keep recovery inside the existing Image Studio worker and repository. Treat the persisted usage log as the durable billing receipt, persist the selected subscription ID in the existing JSONB payload, heartbeat active generation work, and classify unrecoverable settlement errors into a terminal state.

**Tech Stack:** Go, PostgreSQL, sqlmock, testify, existing Sub2API usage billing and Image Studio services.

---

### Task 1: Use Persisted Usage Logs as Billing Receipts

**Files:**
- Modify: `backend/internal/service/account_usage_service.go`
- Modify: `backend/internal/repository/usage_log_repo_query.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Test: `backend/internal/repository/usage_log_repo_unit_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`
- Test: `backend/internal/service/image_studio_job_worker_test.go`

- [ ] **Step 1: Write failing repository and recorder tests**

Add tests proving that `GetByRequestIDAndAPIKey` scans the existing usage row and that a duplicate billing result returns that persisted row instead of a newly calculated balance row.

```go
func TestUsageLogRepositoryGetByRequestIDAndAPIKey(t *testing.T) {
	// Expect SELECT <usageLogSelectColumns> FROM usage_logs
	// WHERE request_id = $1 AND api_key_id = $2.
	// Return a usage-card row and assert BillingTypeUsageCard and ActualCost.
}

func TestOpenAIGatewayRecordUsageDetailedDuplicateReturnsExistingReceipt(t *testing.T) {
	// Billing Apply returns Applied:false.
	// Usage lookup returns the original usage-card log.
	// Assert the returned pointer is the persisted receipt and Create is not called.
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd backend
go test -tags=unit ./internal/repository ./internal/service \
  -run 'GetByRequestIDAndAPIKey|DuplicateReturnsExistingReceipt' -count=1
```

Expected: compilation fails because the repository method does not exist and duplicate billing still writes a fresh log.

- [ ] **Step 3: Add the lookup and duplicate-receipt path**

Extend the interface and SQL repository:

```go
GetByRequestIDAndAPIKey(ctx context.Context, requestID string, apiKeyID int64) (*UsageLog, error)
```

Implement it with `usageLogSelectColumns` and `scanUsageLog`. In `recordUsageDetailed`, retain the boolean returned by `applyUsageBilling`:

```go
applied, err := applyUsageBilling(...)
if err != nil {
	return nil, err
}
if !applied {
	receipt, err := s.usageLogRepo.GetByRequestIDAndAPIKey(ctx, requestID, apiKey.ID)
	if err != nil {
		return nil, fmt.Errorf("load duplicate usage receipt: %w", err)
	}
	return receipt, nil
}
```

Do not call `writeUsageLogBestEffort` for the duplicate branch.

- [ ] **Step 4: Add Image Studio preflight receipt lookup**

Before loading mutable API key, account, subscription, or pricing state, look up `image-studio-job:<jobID>` using `job.APIKeyID`. If found, call `MarkSucceeded` with the receipt's `ActualCost`. Continue normal settlement only when `ErrUsageLogNotFound` is returned.

- [ ] **Step 5: Run focused tests and commit**

```bash
cd backend
go test -tags=unit ./internal/repository ./internal/service \
  -run 'GetByRequestIDAndAPIKey|DuplicateReturnsExistingReceipt|ImageStudio.*ExistingReceipt' -count=1
git add internal/service/account_usage_service.go internal/repository/usage_log_repo_query.go \
  internal/service/openai_gateway_usage.go internal/service/image_studio_job_worker.go \
  internal/repository/usage_log_repo_unit_test.go internal/service/openai_gateway_record_usage_test.go \
  internal/service/image_studio_job_worker_test.go
git commit -m "fix: reuse persisted image studio billing receipts"
```

### Task 2: Persist the Selected Subscription

**Files:**
- Modify: `backend/internal/service/image_studio_job_service.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Test: `backend/internal/service/image_studio_job_worker_test.go`

- [ ] **Step 1: Write failing payload and retry tests**

```go
func TestImageStudioSettlementPayloadPreservesSubscriptionID(t *testing.T) {
	// Marshal with subscription ID 91 and assert round-trip equality.
}

func TestImageStudioSettlementUsesPersistedSubscriptionAfterExpiry(t *testing.T) {
	// GetByID returns subscription 91; GetActiveSubscription must not be called.
	// Assert UsageBillingCommand.SubscriptionID == 91.
}
```

- [ ] **Step 2: Run tests and verify RED**

```bash
cd backend
go test -tags=unit ./internal/service -run 'ImageStudio.*Subscription' -count=1
```

Expected: payload has no subscription ID and settlement only calls `GetActiveSubscription`.

- [ ] **Step 3: Extend the JSON payload and resolver**

Add the optional field without changing the payload version:

```go
SubscriptionID *int64 `json:"subscription_id,omitempty"`
```

Extend the private resolver interface:

```go
type imageStudioSubscriptionResolver interface {
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetActiveSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
}
```

Pass the preflight subscription into payload marshaling. During settlement, use `GetByID` when `subscription_id` exists and validate its user/group ownership. Use active lookup only for old payloads without the field.

- [ ] **Step 4: Run tests and commit**

```bash
cd backend
go test -tags=unit ./internal/service -run 'ImageStudio.*Subscription|SettlementPayload' -count=1
git add internal/service/image_studio_job_service.go internal/service/image_studio_job_worker.go \
  internal/service/image_studio_job_worker_test.go
git commit -m "fix: persist image studio subscription selection"
```

### Task 3: Recover Interrupted Running Jobs Without Upstream Replay

**Files:**
- Modify: `backend/internal/service/image_studio_job.go`
- Modify: `backend/internal/repository/image_studio_job_repo.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Test: `backend/internal/repository/image_studio_job_repo_test.go`
- Test: `backend/internal/service/image_studio_job_worker_test.go`
- Test: repository stubs in `backend/internal/service/image_studio_job_service_test.go` and `backend/internal/handler/image_studio_job_handler_test.go`

- [ ] **Step 1: Write failing repository tests**

Cover runnable selection of stale `running`, heartbeat updates guarded by `status=running`, and a compare-and-set transition to failed:

```go
MarkStaleRunningFailed(ctx context.Context, id int64, completedAt, staleBefore time.Time) (bool, error)
```

The SQL must require both `status = running` and `heartbeat_at <= staleBefore`.

- [ ] **Step 2: Write failing worker tests**

```go
func TestImageStudioStaleRunningJobIsFailedWithoutUpstreamReplay(t *testing.T) {
	// Pass a stale running job to processJob.
	// Assert MarkRunning and forward dependencies are untouched.
	// Assert MarkStaleRunningFailed is called once.
}

func TestImageStudioRunningJobRefreshesHeartbeat(t *testing.T) {
	// Use a short injected heartbeat interval and block forwarding.
	// Assert at least one UpdateHeartbeat call before release.
}
```

- [ ] **Step 3: Run tests and verify RED**

```bash
cd backend
go test -tags=unit ./internal/repository ./internal/service -run 'ImageStudio.*(StaleRunning|Heartbeat)' -count=1
```

- [ ] **Step 4: Implement guarded recovery and active heartbeat**

Include stale `running` rows in `ListRunnableJobs`. Branch before `MarkRunning`:

```go
if job.Status == ImageStudioJobStatusRunning {
	s.recoverStaleRunningJob(ctx, job)
	return
}
```

Start a ticker after `MarkRunning` and stop it when processing exits. Update heartbeat only while the database row remains `running`; a concurrent `running -> settling` transition must not receive later heartbeat writes.

- [ ] **Step 5: Run tests and commit**

```bash
cd backend
go test -tags=unit ./internal/repository ./internal/service ./internal/handler -run ImageStudio -count=1
git add internal/service/image_studio_job.go internal/repository/image_studio_job_repo.go \
  internal/service/image_studio_job_worker.go internal/repository/image_studio_job_repo_test.go \
  internal/service/image_studio_job_worker_test.go internal/service/image_studio_job_service_test.go \
  internal/handler/image_studio_job_handler_test.go
git commit -m "fix: recover interrupted image studio workers"
```

### Task 4: Terminate Invalid Settlements and Clean Uncommitted Assets

**Files:**
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Test: `backend/internal/service/image_studio_job_worker_test.go`

- [ ] **Step 1: Write failing terminal-error tests**

Add table tests for malformed payload, missing API key, missing account, missing persisted subscription, and ownership mismatch. Assert `MarkFailed` is called and `MarkSettlementRetryable` is not called. Add a temporary database error case asserting the inverse.

- [ ] **Step 2: Write failing filesystem cleanup test**

Use `t.TempDir()` as the Image Studio asset root, persist a valid test image, force `MarkSettling` to fail, and assert the job asset directory no longer exists.

- [ ] **Step 3: Run tests and verify RED**

```bash
cd backend
go test -tags=unit ./internal/service \
  -run 'ImageStudio.*(TerminalSettlement|RetryableSettlement|CleansAssets)' -count=1
```

- [ ] **Step 4: Implement error classification and cleanup ownership**

Introduce package-private sentinel errors for invalid payload and invalid settlement dependencies. Route them through:

```go
if isTerminalImageStudioSettlementError(err) {
	s.failJob(ctx, job.ID, "settlement_unrecoverable", err)
	return
}
s.requeueSettlement(ctx, job, err)
```

After asset persistence, remove the per-job directory on payload or `MarkSettling` failure. Set `assetsCommitted = true` only after `MarkSettling` succeeds.

- [ ] **Step 5: Run tests and commit**

```bash
cd backend
go test -tags=unit ./internal/service -run ImageStudio -count=1
git add internal/service/image_studio_job_worker.go internal/service/image_studio_job_worker_test.go
git commit -m "fix: converge invalid image studio settlements"
```

### Task 5: Full Verification

**Files:**
- Verify all files modified in Tasks 1-4.

- [ ] **Step 1: Format and inspect the diff**

```bash
cd backend
gofmt -w internal/service/account_usage_service.go internal/repository/usage_log_repo_query.go \
  internal/service/openai_gateway_usage.go internal/service/image_studio_job.go \
  internal/service/image_studio_job_service.go internal/service/image_studio_job_worker.go \
  internal/repository/image_studio_job_repo.go internal/repository/usage_log_repo_unit_test.go \
  internal/service/openai_gateway_record_usage_test.go internal/repository/image_studio_job_repo_test.go \
  internal/service/image_studio_job_worker_test.go internal/service/image_studio_job_service_test.go \
  internal/handler/image_studio_job_handler_test.go
git diff --check
```

- [ ] **Step 2: Run static and full unit verification**

```bash
cd backend
go vet ./internal/service ./internal/repository ./internal/handler
go test -tags=unit ./...
```

- [ ] **Step 3: Run migration and server checks**

```bash
cd backend
go test ./migrations ./internal/server ./cmd/server -count=1
```

- [ ] **Step 4: Verify conflict scope and secrets**

```bash
git status --short
git diff main...HEAD -- backend/internal/service/image_studio_job.go \
  backend/internal/service/image_studio_job_service.go \
  backend/internal/service/image_studio_job_worker.go \
  backend/internal/repository/image_studio_job_repo.go \
  backend/internal/service/account_usage_service.go \
  backend/internal/repository/usage_log_repo_query.go \
  backend/internal/service/openai_gateway_usage.go
```

Confirm `api_key` is untracked and unstaged, no frontend/generated schema files changed, and production changes remain within the design's listed files.
