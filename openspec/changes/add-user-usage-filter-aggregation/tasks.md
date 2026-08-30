## 1. Database aggregation foundation

- [x] 1.1 Add a migration for the hourly user-usage aggregate table, readiness state, indexes, and normalized metric columns.
- [x] 1.2 Add statement-level INSERT, UPDATE, and DELETE trigger functions that maintain exact aggregate deltas.

## 2. Isolated analytics implementation

- [x] 2.1 Add a private ordinary-user usage request context marker and middleware without changing API contracts.
- [x] 2.2 Implement the aggregate store with readiness/coverage checks, parity-safe filter builders, exact totals/statistics queries, and resumable segmented backfill.
- [x] 2.3 Implement a repository decorator that transparently accelerates supported marked queries and delegates every fallback or detail-row operation to the legacy repository.
- [x] 2.4 Wire the middleware and repository decorator through the two minimal existing-code integration points.

## 3. Correctness and regression coverage

- [x] 3.1 Add differential tests for aggregate versus legacy totals/statistics across supported filter combinations and fallback conditions.
- [x] 3.2 Add trigger/backfill lifecycle tests covering insert, update, delete, readiness, coverage, interruption, and resume behavior.
- [x] 3.3 Run formatting, focused backend tests, migration validation, and document the final original-file conflict surface.

## Verification Notes (2026-08-30)

The production merge-conflict surface is limited to two existing files: one return-line replacement in `backend/internal/repository/usage_log_repo.go` and one middleware line in `backend/internal/server/routes/user.go`. All other production implementation, migration, and test code is in newly added files.

Formatting, focused repository/route/migration tests, build-tag integration compilation, `go vet`, and `git diff --check` pass. Docker is unavailable in the local workspace, so PostgreSQL-backed integration tests compile but could not execute here. A full `go test ./...` reached and passed all affected packages, then hit the unrelated existing/flaky service test `TestCanonicalOpenAIAccountSchedulingModelMatchesForwardSemantics/Grok_OAuth_does_not_inherit_OpenAI_Codex_aliases`; that test passes when rerun in isolation.
