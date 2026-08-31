## 1. Repository composition and conflict reduction

- [x] 1.1 Add a single `usageAggregationRepository` decorator that owns and starts the four independent aggregation Stores while embedding the unchanged base repository.
- [x] 1.2 Move Account, API Key, Dashboard, and ordinary-user analytics fast-path overrides into the decorator and restore statistic-specific changes in main-origin Repository/Service files wherever behavior permits.
- [x] 1.3 Add compile-time and constructor tests proving the decorator preserves the shared repository and optional interfaces.

## 2. Account hybrid-read compatibility release

- [x] 2.1 Add a forward migration for Account `closed_before` state and `account_usage_stats_dirty_days` metadata without removing the existing synchronous Account triggers.
- [x] 2.2 Implement a single-snapshot Account query that combines non-dirty closed daily aggregates with raw open-tail and dirty-day rows, preserving all existing dimensions and response calculations.
- [x] 2.3 Convert Account background work to bounded continuous maintenance with advisory locks, a shared usage-maintenance lock, disabled query parallelism, one daily bucket per cycle, and a 20-second statement timeout.
- [x] 2.4 Add Account unit/integration and migration tests for hybrid ranges, dirty-day fallback, watermark advancement, cross-midnight completion semantics, error fallback, and trigger retention in stage one.

## 3. Local verification and merge-surface audit

- [x] 3.1 Run formatting, focused repository/migration tests, integration-test compilation, `go vet` for affected packages, OpenSpec validation, and `git diff --check`.
- [x] 3.2 Compare the final implementation with `origin/main@6ca1e15b0` and document every remaining changed line in main-origin production files.

### Verification notes (2026-08-31)

- `go test ./internal/repository ./migrations` passed.
- Targeted Account/analytics/service tests, handler tests, and route tests passed; integration test binary compilation passed.
- `go vet ./internal/repository ./migrations ./internal/server/routes` passed; OpenSpec strict validation and `git diff --check` passed.
- PostgreSQL integration execution was not possible locally because rootless Docker is unavailable.
- Compared with `origin/main@6ca1e15b0`, the new production conflict surface is one constructor line in `backend/internal/repository/usage_log_repo.go`; `backend/internal/repository/usage_log_repo_stats.go` and `usage_log_repo_dashboard.go` are restored to the origin/main content. The existing user-usage middleware line and unrelated pre-existing branch changes remain outside this change.

## 4. Account write-path cutover gate

- [ ] 4.1 Deploy the stage-one compatibility version to HC2 and verify Account raw-versus-hybrid parity plus database CPU, WAL, write latency, and aggregate-table update counts.
- [ ] 4.2 After stage-one verification, add a separate forward migration that removes only the Account synchronous delta triggers/function and installs historical-change dirty-day triggers.
- [ ] 4.3 Deploy the Account cutover to HC2 and verify normal current usage no longer updates `account_usage_stats_daily`, historical mutations remain exact, and the old compatible slot is the rollback target.

## 5. Sequential rollout of remaining custom statistics

- [ ] 5.1 Apply the same closed-hour hybrid-read lifecycle to `user_usage_analytics_hourly`, with no more than two closed hours per maintenance cycle.
- [ ] 5.2 Apply the same closed-day lifecycle to `user_dashboard_route_daily` after recent-usage production observation passes.
- [ ] 5.3 Apply the same closed-day lifecycle to `api_key_usage_daily` and remove its usage-log INSERT CTE only after the previous rollouts pass.
- [ ] 5.4 Run final parity and resource verification for all four statistics and record the safe rollback chain.
