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

- [x] 4.1 Deploy the stage-one compatibility version to HC2 and verify Account raw-versus-hybrid parity plus database CPU, WAL, write latency, and aggregate-table update counts.
- [x] 4.2 After stage-one verification, add a separate forward migration that removes only the Account synchronous delta triggers/function and installs historical-change dirty-day triggers.
- [x] 4.3 Deploy the Account cutover to HC2 and verify normal current usage no longer updates `account_usage_stats_daily`, historical mutations remain exact, and the old compatible slot is the rollback target.

## 5. Sequential rollout of remaining custom statistics

- [ ] 5.1 Apply the same closed-hour hybrid-read lifecycle to `user_usage_analytics_hourly`, with no more than two closed hours per maintenance cycle.
- [ ] 5.2 Apply the same closed-day lifecycle to `user_dashboard_route_daily` after recent-usage production observation passes.
- [ ] 5.3 Apply the same closed-day lifecycle to `api_key_usage_daily` and remove its usage-log INSERT CTE only after the previous rollouts pass.
- [ ] 5.4 Run final parity and resource verification for all four statistics and record the safe rollback chain.

### HC2 stage-one verification (2026-08-31)

- Deployed `sub2api:hc2-20260831232219-22dc8945e15c-amd64` to `sub2api-canary:18081` with a graceful Nginx switch from `18082`; both slots and both public hosts remained healthy throughout.
- Migration `234_account_usage_stats_hybrid_read.sql` applied at `2026-08-31 23:25:13 +08:00`; both Account synchronous triggers remain installed, `closed_before` advanced online to `2026-08-31`, and no historical rebuild or aggregate-table lock ran.
- A two-day closed-history plus open-tail comparison for Account `6703` returned 8 legacy rows, 8 hybrid rows, and 0 mismatches across requests, tokens, costs, durations, models, and endpoints.
- Startup-to-post-cutover counters showed no aggregate rebuild inserts; normal usage continued to produce about four `account_usage_stats_daily` updates per new usage row, confirming the synchronous Account trigger remains the dominant write amplification to remove in stage two.
- PostgreSQL has neither `pg_stat_statements` nor `track_io_timing` enabled. A 118-second post-cutover sample used `pg_stat_database.active_time` as the bounded workload proxy (about 10.8% of one-core wall time), observed about 52.9 KB/s WAL, zero lock waiters, and no usage-record timeout/drop or database/Redis/migration errors.

### HC2 Account write-path cutover verification

- Deployed `sub2api:hc2-20260901000808-8c33d9d6a951-amd64` to `sub2api-next:18082` while the stage-one-compatible `sub2api-canary:18081` continued serving, then switched Nginx with a successful configuration test and graceful reload.
- Migration `235_account_usage_stats_write_path_cutover.sql` applied at HC2 server time `2026-09-01 00:11:43 +08:00`. The two synchronous Account delta triggers and `apply_account_usage_stats_daily_delta()` are absent; exactly three Account dirty-day triggers are installed.
- During a real-traffic window from `00:16:15` to `00:21:43`, `usage_logs.n_tup_ins` increased by 85 while Account aggregate inserts, updates, and deletes did not change. A preceding 60-second window similarly added 38 usage rows with zero Account aggregate updates; separate aggregate insert/delete activity was attributable only to bounded repair of two retention-dirty historical dates.
- A production transaction probe against usage log `5491403` marked `2026-08-31` dirty after both UPDATE and DELETE; `ROLLBACK` restored dirty rows to zero and preserved the source row, confirming exact historical-change fallback without persistent test data.
- Both public hosts and both slots remained healthy, AIXW homepage configuration remained correct, lock waiters stayed at zero, and no migration/database/Redis/maintenance or usage-record-drop errors appeared. The preserved `18081` stage-one image is the direct rollback target.
