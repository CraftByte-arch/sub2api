## 1. Planning and data contract

- [x] 1.1 Add online-user and per-user consumption models with JSON contract tests.
- [x] 1.2 Add client methods for Ops request pagination, current-day usage fallback, user identity lookup, and batch today-usage lookup with bounded timeouts/concurrency.
- [x] 1.3 Extend the online-user contract with per-group distinct counts and explicit available/partial metadata.

## 2. Sidecar HTTP API

- [x] 2.1 Add `GET /api/online-users` behind the existing administrator middleware and aggregate the ten-minute window.
- [x] 2.2 Return partial-data/truncation metadata without failing the existing `/api/overview` endpoint.
- [x] 2.3 Add focused handler/client tests for deduplication, fallback, auth, partial usage, and empty results.
- [x] 2.4 Aggregate request-time group membership without changing Sub2API or connecting to its database.

## 3. Administrator UI

- [x] 3.1 Add an accessible online-user metric to the overview metrics band.
- [x] 3.2 Add a responsive dialog listing user, last call time, today's cost, and tokens, with loading/empty/error/retry states.
- [x] 3.3 Integrate ten-second refresh and preserve existing cache-hit-rate and account controls.
- [x] 3.4 Render a responsive, accessible online-count badge in every group header, including zero/loading/partial/unavailable states.

## 4. Verification

- [x] 4.1 Run Go formatting, unit tests, and static checks for the sidecar.
- [x] 4.2 Run frontend/static contract tests and responsive browser validation.
- [x] 4.3 Build the sidecar artifact and document that HC2 deployment is intentionally deferred until requested.
- [x] 4.4 Add per-group aggregation, JSON/API contract, static UI, and frontend runtime projection tests.

## Verification note

The sidecar binary was built and validated locally, including per-group aggregation and desktop/390px responsive checks. HC2 deployment is intentionally deferred; this change only updates the isolated sidecar branch and does not modify or restart Sub2API.
