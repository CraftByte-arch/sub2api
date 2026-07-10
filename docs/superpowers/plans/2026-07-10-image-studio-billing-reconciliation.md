# Image Studio Billing Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the pre-merge usage-card billing semantics and move Image Studio jobs onto the unified, idempotent gateway billing path.

**Architecture:** Gateway usage recording remains the single owner of pricing, wallet selection, subscription accounting, quota updates, cache synchronization, notifications, and usage logs. Image Studio persists generated assets, transitions into a retryable settlement stage, and records usage with a stable job-scoped request ID so settlement retries cannot double charge.

**Tech Stack:** Go, PostgreSQL migrations, sqlmock, testify, Wire, existing Sub2API billing and image-studio services.

---

### Task 1: Restore Usage-Card Post-Billing Semantics

**Files:**
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Test: `backend/internal/service/billing_cache_service_balance_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`

- [x] **Step 1: Write failing tests for usage-card side effects**

Add tests that pass `UsageBillingApplyResult{UsageCardID: &id}` and assert that balance cache deduction and low-balance notification are not invoked. Extend the usage-card record test to assert `BillingTypeUsageCard`.

- [x] **Step 2: Run the focused tests and verify RED**

Run:

```bash
go test -tags=unit ./internal/service -run 'UsageCard|SyncBalanceCacheAfterDeduction' -count=1
```

Expected: failures showing a balance-cache deduction and `BillingTypeBalance`.

- [x] **Step 3: Restore the pre-merge branches**

When `result.UsageCardID != nil`, set both `usageLog.UsageCardID` and `usageLog.BillingType = BillingTypeUsageCard`. Skip balance cache synchronization and balance-low notification for that result while preserving API-key, account, and platform quota updates.

- [x] **Step 4: Run the focused tests and verify GREEN**

Run the command from Step 2 and expect all selected tests to pass.

- [x] **Step 5: Commit the merge regression fix**

```bash
git add backend/internal/service/gateway_usage_billing.go backend/internal/service/billing_cache_service_balance_test.go backend/internal/service/openai_gateway_record_usage_test.go
git commit -m "fix: restore usage card post-billing semantics"
```

### Task 2: Expose Unified Usage Results and Image Estimates

**Files:**
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/service/image_studio_job_service.go`
- Modify: `backend/internal/handler/image_studio_job_handler.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`
- Test: `backend/internal/handler/image_studio_job_handler_test.go`

- [x] **Step 1: Write failing tests for detailed usage and default image pricing**

Add a test proving the internal detailed recorder returns the persisted `UsageLog`, including `ActualCost` and billing source. Add a handler/service estimate test proving a group with nil image prices receives the same non-zero default/model image price as the gateway path.

- [x] **Step 2: Run focused tests and verify RED**

```bash
go test -tags=unit ./internal/service ./internal/handler -run 'RecordUsageDetailed|ImageStudio.*Estimate' -count=1
```

Expected: failures because the detailed recorder and unified estimator do not exist.

- [x] **Step 3: Refactor without changing normal gateway behavior**

Keep `RecordUsage(ctx, input) error` as the public compatibility wrapper and move its implementation to `recordUsageDetailed(ctx, input) (*UsageLog, error)`. Add an Image Studio estimate method that builds a one-image synthetic result and calls the same multiplier and image-cost functions used by real gateway usage.

- [x] **Step 4: Replace the handler's local estimator**

Call `jobService.EstimateCost(...)` and remove `estimateImageStudioJobCost` and `resolveImageStudioMultiplier`.

- [x] **Step 5: Run focused tests and verify GREEN**

Run the command from Step 2 and expect all selected tests to pass.

### Task 3: Add a Retryable Settlement Stage

**Files:**
- Create: `backend/migrations/173_image_studio_settlement_payload.sql`
- Modify: `backend/internal/service/image_studio_job.go`
- Modify: `backend/internal/repository/image_studio_job_repo.go`
- Test: `backend/internal/repository/image_studio_job_repo_test.go`

- [x] **Step 1: Write failing repository tests**

Cover these transitions:

```text
running -> settling
settling -> settling retry
settling -> succeeded
```

Assert that entering settlement stores asset metadata and a durable billing snapshot before billing, and that runnable selection includes due settlement jobs while excluding actively leased settlement jobs.

- [x] **Step 2: Run repository tests and verify RED**

```bash
go test -tags=unit ./internal/repository -run 'ImageStudio.*Sett' -count=1
```

- [x] **Step 3: Implement model and repository transitions**

Add the `settling` status to the service model. Add a JSONB `settlement_payload` column because a retry must restore the selected account, actual forward result, channel attribution, and endpoint metadata without invoking upstream generation again. `MarkSettling` stores that snapshot with deterministic asset paths and sets a settlement lease heartbeat. `ClaimSettling` reclaims only nil or stale leases. `MarkSettlementRetryable` keeps the job in `settling`, clears the lease, increments attempts, and records the next retry time.

- [x] **Step 4: Run repository tests and verify GREEN**

Run the command from Step 2 and the complete image-studio repository tests.

### Task 4: Settle Image Studio Through Unified Billing

**Files:**
- Modify: `backend/internal/service/image_studio_job_service.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire_gen.go` (generated)
- Test: `backend/internal/service/image_studio_job_service_test.go`
- Test: `backend/internal/service/image_studio_job_worker_test.go`

- [x] **Step 1: Write failing worker tests**

Cover:

```text
subscription group resolves and forwards an active subscription
asset persistence completes before usage recording
settlement uses request_id image-studio-job:<id>
settlement retry does not invoke upstream generation
usage-card settlement does not touch balance cache
MarkSucceeded errors return the job to settling
```

- [x] **Step 2: Run focused worker tests and verify RED**

```bash
go test -tags=unit ./internal/service -run 'ImageStudio.*(Settlement|Subscription|Billing)' -count=1
```

- [x] **Step 3: Implement unified settlement**

Make forwarding return the selected account and channel mapping metadata. Persist assets and the complete settlement snapshot, mark the job settling, resolve the active subscription for subscription groups, set `result.RequestID = fmt.Sprintf("image-studio-job:%d", job.ID)`, and call `recordUsageDetailed` with `HashUsageRequestPayload(job.RequestPayload)`. Use `usageLog.ActualCost` when marking the job succeeded. A completion-write failure remains in `settling`; the stable request ID makes billing retries idempotent.

- [x] **Step 4: Remove direct wallet and quota mutation**

Delete `chargeJob`, direct `UserRepository.DeductBalance`, direct `UsageCardService.DeductFirstAvailable`, manual API-key quota updates, and manual balance-cache deduction from the Image Studio worker.

- [x] **Step 5: Regenerate Wire and verify GREEN**

```bash
cd backend/cmd/server && go generate
cd ../.. && go test -tags=unit ./internal/service -run 'ImageStudio|UsageCard' -count=1
```

### Task 5: Full Verification and Commit

**Files:**
- Verify all modified backend files and generated wiring.

- [x] **Step 1: Run formatting and static checks**

```bash
gofmt -w \
  internal/service/gateway_usage_billing.go \
  internal/service/openai_gateway_usage.go \
  internal/service/image_studio_job.go \
  internal/service/image_studio_job_service.go \
  internal/service/image_studio_job_worker.go \
  internal/service/wire.go \
  internal/repository/image_studio_job_repo.go \
  internal/handler/image_studio_job_handler.go \
  internal/service/billing_cache_service_balance_test.go \
  internal/service/openai_gateway_record_usage_test.go \
  internal/service/image_studio_job_service_test.go \
  internal/service/image_studio_job_worker_test.go \
  internal/repository/image_studio_job_repo_test.go \
  internal/handler/image_studio_job_handler_test.go
go vet ./internal/service ./internal/repository ./internal/handler ./internal/handler/admin
```

- [x] **Step 2: Run the full backend unit suite**

```bash
go test -tags=unit ./...
```

- [x] **Step 3: Run migration and API contract tests**

```bash
go test ./migrations ./internal/server -count=1
```

- [x] **Step 4: Review the final diff**

Confirm the diff contains no secret files, no unrelated generated churn, and no direct Image Studio wallet mutations.

- [ ] **Step 5: Commit the Image Studio settlement fix**

```bash
git add backend docs/superpowers/plans/2026-07-10-image-studio-billing-reconciliation.md
git commit -m "fix: unify image studio billing settlement"
```
