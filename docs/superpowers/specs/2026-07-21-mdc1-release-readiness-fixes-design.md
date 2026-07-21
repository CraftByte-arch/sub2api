# MDC1 Release Readiness Fixes Design

**Date:** 2026-07-21

## Context

The monitoring feature on `codex/feature/20260719/new-requirement` is functionally complete, but release review found four issues that must be resolved before deploying to MDC1:

1. Account monitoring search promises name-or-ID matching, while the repository query only searches account names.
2. `MonitoringTrendChart` contains hard-coded Chinese loading and empty-state text.
3. A prior merge retained image-routing regression tests while dropping the corresponding production fix, and several related tests still encode older image-tool semantics.
4. The deployment skill syncs the entire local workspace, which can copy ignored secrets, worktrees, binaries, and build artifacts to the server.

## Approach

### Monitoring search

Keep the existing single search parameter and extend the SQL predicate to match either `account_name` or the textual form of `account_id`. This preserves pagination and parameter ordering while making the API behavior match the UI contract.

### Chart localization

Make the chart own its default loading and empty labels through `vue-i18n`. Add `admin.monitoring.trends.loading` to every supported locale and reuse the existing `admin.monitoring.trends.empty` key. Named slots remain available for callers that need custom content.

### Image routing and backend contracts

Restore the existing, previously validated image-routing fix from commit `13ec3a6b1` against the current branch:

- Carry image protocol preference through both the advanced scheduler and load-aware fallback paths.
- Rank exact protocol matches ahead of automatic and mismatched accounts without changing ranking inside each tier.
- Resolve the canonical image-intent hint before attempt-local request mutation, and let passthrough gating reuse that canonical value.
- Treat stripped passive declarations as attempt-local invalidation so they can continue as text requests without retaining image billing intent.

Update tests to reflect those production paths:

- Treat a passively declared image tool as removable capability, not image-generation intent. Keep rejection coverage for explicit image requests and add forwarding coverage for the passive-tool case.
- Inspect the helper that now owns stream idle-timeout classification instead of its wrapper.
- Require explicit tool selection where tests intend to exercise image generation, and expect Codex image namespaces to be canonicalized into the hosted tool when the bridge is active.

These changes restore protocol-aware production routing and align the surrounding tests with the implemented request pipeline.

### Release synchronization

Build a temporary deployment tree from `git archive HEAD`, then use `rsync --delete` from that tree. Only committed content reaches the remote build directory, so ignored environment files, local worktrees, caches, and binaries cannot leak into the Docker build context. The same rule applies to MDC1 and MDC2.

## Verification

Run focused tests first, then the complete backend and frontend verification suites. Confirm the deployment source is a clean committed revision before building a unique image tag. Deploy to the inactive MDC1 slot, validate health, homepage variant, container state, and logs, then switch nginx only after all standby checks pass. Keep the former active container running as the immediate rollback target.
