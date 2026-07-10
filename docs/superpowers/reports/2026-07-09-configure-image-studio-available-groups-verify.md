# configure-image-studio-available-groups Verify Report

Date: 2026-07-10
Branch: codex/feature/20260709/configure-image-studio-available-groups
Result: PASS

## Scope

- Added admin image studio available group allowlist.
- Added client `image_generation` tool declaration policy.
- Enforced image studio group allowlist in backend job creation and worker checks.
- Filtered image studio key choices by public allowlist and existing image eligibility.
- Split passive image tool declaration from actual image-generation intent for HTTP Responses and Responses WebSocket.
- Reconciled the feature with the merged `main` service, settings, scheduling, billing, and WebSocket boundaries.
- Resolved post-merge review findings for usage-card log attribution, Image Studio billing-switch enforcement, and WebSocket declaration-policy ordering.

## OpenSpec Verification

- `openspec status --change configure-image-studio-available-groups --json`: complete.
- `openspec validate --type change configure-image-studio-available-groups --json`: 1/1 valid with no issues.
- `openspec validate configure-image-studio-available-groups --strict`: passed.
- `openspec/changes/configure-image-studio-available-groups/tasks.md`: 0 incomplete tasks.
- Delta spec, proposal, OpenSpec design, and Superpowers design doc are consistent with the implementation.

## Test Evidence

- Focused red/green regression tests reproduced and fixed all three post-merge review findings.
- `cd backend && go test -tags unit ./internal/service -run 'UsageCard|ImageStudio|ImageGeneration|DeclarationPolicy|ProxyResponsesWebSocketFromClient' -count=1`: passed.
- `cd backend && go test -tags unit ./... -count=1`: passed; `internal/service` completed in 92.458 seconds.
- `cd frontend && pnpm vitest run`: passed, 154 files and 1058 tests.
- `cd frontend && pnpm run typecheck`: passed.
- `cd frontend && pnpm run lint:check`: passed with 0 errors and 3 pre-existing unused-import warnings in `AixwHomeView.spec.ts`.
- `cd frontend && pnpm run build`: passed with existing dynamic-import and chunk-size warnings.
- Comet build guard executed `make build`: passed and advanced the change to `verify`.
- Comet verify guard executed `make build`: passed and advanced the change to `archive` with `verify_result: pass`.

## Review Evidence

- Initial post-merge review found 0 Critical and 3 Important issues.
- All three Important issues were fixed with regression tests.
- Post-fix review found no Critical, Important, or Minor findings.
- `git diff --check`: passed.

## Branch Handling

User selected: keep current branch as-is and do not touch `main`.

Current branch is retained for later handling:
`codex/feature/20260709/configure-image-studio-available-groups`

## Residual Notes

- Untracked file `api_key` existed before finalization and was not touched.
- Existing repository tests cover `usage_logs.usage_card_id` INSERT/SELECT/scan, while the new service regression test covers propagation from the billing result into the log object; there is no single database integration test spanning both layers.
