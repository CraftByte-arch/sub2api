# configure-image-studio-available-groups Verify Report

Date: 2026-07-09
Branch: codex/feature/20260709/configure-image-studio-available-groups
Result: PASS

## Scope

- Added admin image studio available group allowlist.
- Added client `image_generation` tool declaration policy.
- Enforced image studio group allowlist in backend job creation and worker checks.
- Filtered image studio key choices by public allowlist and existing image eligibility.
- Split passive image tool declaration from actual image-generation intent for HTTP Responses and Responses WebSocket.

## OpenSpec Verification

- `openspec status --change "configure-image-studio-available-groups" --json`: complete.
- `openspec validate --type change "configure-image-studio-available-groups" --json`: passed.
- `openspec/changes/configure-image-studio-available-groups/tasks.md`: 0 incomplete tasks.
- Delta spec, proposal, OpenSpec design, and Superpowers design doc are consistent with the implementation.

## Test Evidence

- `cd backend && go test -tags unit ./internal/service ./internal/handler ./internal/handler/admin -run 'ImageStudio|ImageGeneration|DeclarationPolicy|Settings|OpenAIGatewayService_Forward_DecodedMutationKeepsLaterFieldDeletes|OpenAIGatewayService_Forward_ImageToolBillingDoesNotForceFullDecode' -count=1`: passed.
- `cd frontend && pnpm vitest run src/extensions/image-studio/__tests__/ImageStudioView.spec.ts src/views/admin/__tests__/SettingsView.spec.ts`: passed, with existing `router-link` test warnings.
- `cd frontend && pnpm typecheck`: passed.
- `cd backend && go test ./...`: passed.

## Branch Handling

User selected: keep current branch as-is and do not touch `main`.

Current branch is retained for later handling:
`codex/feature/20260709/configure-image-studio-available-groups`

## Residual Notes

- Untracked file `api_key` existed before finalization and was not touched.
