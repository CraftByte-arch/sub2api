# Image Studio Settlement Recovery Design

## Context

Image Studio now separates upstream generation from billing with a `settling` state. The current implementation prevents duplicate upstream generation, but recovery still re-resolves mutable billing state and cannot converge when a worker crashes in `running`, a billing dependency is deleted, or asset metadata fails to persist.

The implementation must remain easy to rebase onto upstream `main`. It should extend the existing Image Studio worker and repository rather than introduce a new billing subsystem or refactor the shared billing transaction.

## Goals

- Complete an already-billed Image Studio job without recalculating billing.
- Preserve the subscription selected before upstream generation.
- Recover stale `running` jobs without invoking upstream generation again.
- Stop retrying permanently invalid settlement jobs.
- Remove files written before a failed `running -> settling` transition.
- Preserve correct usage-card attribution on duplicate billing attempts.

## Non-Goals

- No cross-repository transaction combining billing, usage-log insertion, and job completion.
- No full API key, user, group, account, or wallet snapshot in `settlement_payload`.
- No changes to the public API or admin UI.
- No automatic replay of an interrupted upstream generation request.

## Design

### 1. Durable Subscription Selection

Add an optional `subscription_id` to the existing versioned JSON settlement payload. The worker resolves the active subscription before upstream generation and writes its ID alongside the selected account and actual forward result.

Settlement loads that subscription by ID, including an expired but still persisted subscription. Payloads created before this change have no subscription ID and retain the existing active-subscription fallback.

No database migration is required because `settlement_payload` is already JSONB.

### 2. Existing Usage Receipt Lookup

Add one method to `UsageLogRepository` and its SQL implementation:

```go
GetByRequestIDAndAPIKey(ctx context.Context, requestID string, apiKeyID int64) (*UsageLog, error)
```

Before calculating or applying billing, Image Studio looks up `image-studio-job:<jobID>`. If a usage log exists, its persisted `ActualCost` is the billing receipt and the worker only retries `MarkSucceeded`.

The internal detailed OpenAI recorder will retain whether the billing command was newly applied. When the billing repository reports a duplicate, it returns the existing usage log. If the dedup claim exists but the usage log is missing, it returns an explicit reconciliation error and does not insert a new log with an assumed billing source.

This keeps the shared change additive and avoids modifying the usage-billing database schema.

### 3. Stale Running Recovery

While a job is in `running`, the worker periodically refreshes its heartbeat. Runnable selection includes `running` jobs only when their heartbeat is stale. A stale `running` job is claimed with a guarded state transition and marked failed with `worker_interrupted`; it is never sent upstream again because the previous upstream outcome is unknown.

This favors avoiding duplicate generation over silently retrying an uncertain request.

### 4. Retry Classification

Settlement errors are divided into:

- Retryable: temporary database, network, or dependency errors. Keep `settling`, clear the lease, and schedule the next attempt.
- Terminal: malformed settlement payload, missing API key, missing selected account, missing selected subscription, or ownership mismatch. Mark the job failed with a settlement-specific code.

Terminal failures remain visible for manual reconciliation and do not consume worker capacity forever.

### 5. Asset Cleanup

After `persistAssets` succeeds, the worker owns those paths until `MarkSettling` succeeds. If payload encoding or `MarkSettling` fails, it removes both files and the per-job directory before applying the existing retry/failure policy.

Once `MarkSettling` succeeds, normal retention and user deletion own asset cleanup.

## State Flow

```text
queued -> running -> assets persisted -> settling -> succeeded
            |              |                |
            |              |                +-> retry billing/completion only
            |              +-> DB failure: remove new assets, retry/fail
            +-> stale heartbeat: failed(worker_interrupted)
```

## Testing

- Repository tests for usage receipt lookup and stale-running guarded transition.
- Worker tests proving existing usage receipts skip billing and use persisted cost.
- Worker tests proving persisted subscription IDs survive active-subscription changes.
- Worker tests proving stale `running` jobs never call upstream.
- Worker tests for terminal versus retryable settlement errors.
- Filesystem test proving a failed `MarkSettling` removes generated assets.
- Detailed recorder test proving duplicate billing returns the existing usage receipt and never fabricates balance attribution.
- Full backend unit, migration, server, and static verification.

## Conflict Control

Production edits are limited to the existing Image Studio model, worker, repository, their tests, `UsageLogRepository`, and the SQL usage-log query implementation. No generated schema, public handler contract, frontend file, or usage-billing migration is changed.
