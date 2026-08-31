## 1. Configuration and data contracts

- [x] 1.1 Add optional aggregate PostgreSQL DSN configuration and document the dedicated read-only deployment contract.
- [x] 1.2 Add the PostgreSQL driver dependency and bounded connection-pool construction without making database availability a sidecar startup requirement.
- [x] 1.3 Extend online summary/detail models with readiness, watermark, lag, stale state, group counts, and active group memberships.

## 2. Aggregate online-user service

- [x] 2.1 Implement the watermark-anchored ten-minute summary query over `channel_monitor_v2_user_metrics_1m`.
- [x] 2.2 Implement the on-demand detail query with limited user identity columns and ready daily route aggregates.
- [x] 2.3 Implement 60-second deep-copy caches, concurrent miss coalescing, query timeouts, stale last-good behavior, and the ten-minute unavailable cutoff.
- [x] 2.4 Add database-service tests for distinct counting, failures, daily readiness, cache behavior, concurrency, staleness, and absent configuration.

## 3. Sidecar and browser integration

- [x] 3.1 Remove raw HTTP online-user collection and identity cache state from the Sub2API core client.
- [x] 3.2 Inject the optional aggregate service into the web server and add an administrator-protected summary endpoint while preserving the detail endpoint.
- [x] 3.3 Change browser initialization and visible-page polling to load only summaries, reserving details for the online-user dialog and exposing freshness notices.
- [x] 3.4 Update handler, model, and UI contract tests to prove authorization, failure isolation, and summary/detail separation.

## 4. Verification and HC2 release

- [x] 4.1 Run formatting, dependency cleanup, unit tests, race tests, vet, builds, JavaScript syntax checks, and strict OpenSpec validation.
- [ ] 4.2 Commit the complete sidecar-only implementation and confirm the committed tree contains no sliding-window background worker or raw online request-detail calls.
- [ ] 4.3 Create the HC2 dedicated PostgreSQL read-only role with exact grants, back up sidecar state/configuration, and deploy only the committed `account-auto-scheduler` image.
- [ ] 4.4 Verify aggregate summary/detail behavior, administrator authorization, freshness handling, absence of raw online API calls, and unchanged identities/restart counts for every other HC2 container.
