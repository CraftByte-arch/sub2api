# MDC1 Release Readiness Fixes Implementation Plan

> Execute inline on `codex/feature/20260719/new-requirement`. Use test-driven development for behavioral fixes and preserve the current blue-green production topology.

## Task 1: Account ID search regression

1. Extend `TestQueryAccountsAppliesSearchFilter` to require both name and textual account-ID matching.
2. Run the focused repository test and confirm it fails against the existing SQL.
3. Update `account_performance_repo.go` to match `account_name` or `account_id::text` with the same search parameter.
4. Rerun the focused test and repository package tests.

## Task 2: Monitoring chart localization

1. Add a component test that mounts `MonitoringTrendChart` under the English locale and asserts localized loading and empty labels.
2. Run the focused test and confirm the hard-coded Chinese strings fail it.
3. Add the loading translation to all locale files and consume `trends.loading` and `trends.empty` through `useI18n` in the component.
4. Rerun the focused component test.

## Task 3: Image routing and backend contracts

1. Reproduce all failures in the service package and compare them with the previously validated image-routing fix.
2. Restore image protocol preference propagation and ranking in the advanced scheduler and load-aware fallback paths.
3. Resolve canonical image intent before request mutation and reuse it in passthrough permission and billing decisions.
4. Keep disabled-image rejection cases for image models, namespaces, and explicit image-tool selection.
5. Add a valid upstream stub and assert that a passive image-tool declaration is stripped and a text request is forwarded.
6. Update image-generation tests to use explicit tool selection where image intent is required and to expect bridge namespace canonicalization.
7. Point the timeout source-inspection test at `handleStreamingResponseWithReasoning`.
8. Run the affected tests and the full service test package.

## Task 4: Harden deployment source sync

1. Update the MDC1 and MDC2 instructions in `mdc1-update/SKILL.md` to export `HEAD` into a temporary directory.
2. Run `rsync --delete` only from that committed snapshot and remove the temporary directory after sync.
3. Review the resulting commands to ensure no whole-workspace sync remains.

## Task 5: Release verification and commit

1. Run backend service, handler, repository, and build verification.
2. Run frontend tests, lint, and production build.
3. Run `git diff --check` and review the complete diff.
4. Commit the release-readiness fixes so the deployment snapshot is reproducible.

## Task 6: MDC1 blue-green deployment

1. Detect the nginx-routed active slot and choose the other slot as standby.
2. Export the committed revision, sync it to `/home/pyj/sub2api-build/`, and build a unique release image remotely.
3. Recreate the standby container with the active environment and `HOMEPAGE_VARIANT=aixw`.
4. Verify standby health, variant, environment, status, and logs.
5. Back up nginx configuration, switch upstream port, validate configuration, and reload nginx.
6. Verify public health and routing, while leaving the former active container available for rollback.
