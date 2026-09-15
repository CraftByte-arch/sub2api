## 1. Data contract and client

- [x] 1.1 Add group user-consumption snapshot/row models with JSON contract tests.
- [x] 1.2 Add a bounded client method that requests today's group breakdown with a fixed top-20 limit, validates fields, and applies stable sorting.

## 2. Sidecar HTTP API

- [x] 2.1 Add `GET /api/groups/{groupID}/user-consumption` behind the existing administrator middleware.
- [x] 2.2 Return partial-data metadata for missing row fields while keeping existing overview and online-user endpoints independent.
- [x] 2.3 Add handler/client tests for auth, group/date query, sorting/limit, empty results, invalid IDs, and upstream failure.

## 3. Administrator UI

- [x] 3.1 Add a per-group “今日用户 Top 20” action and accessible dialog markup.
- [x] 3.2 Render loading, empty, error/retry, partial-data, user, cost, request, and token states.
- [x] 3.3 Add responsive stacked layout and focus restoration without disturbing existing group/account controls.

## 4. Verification

- [x] 4.1 Run Go formatting, unit tests, race tests, vet, and frontend static checks.
- [x] 4.2 Validate the new button/dialog at desktop and narrow viewport widths.
- [x] 4.3 Build the sidecar artifact and document that HC2 deployment remains deferred until explicitly requested.
