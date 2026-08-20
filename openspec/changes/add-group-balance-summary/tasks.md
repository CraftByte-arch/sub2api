## 1. Overview projection

- [x] 1.1 Add a server-side group balance summary projection using existing account membership, scheduling state, and admin balance semantics.
- [x] 1.2 Include the projection in `/api/overview` without changing existing response fields or Sub2API calls.

## 2. UI

- [x] 2.1 Render enabled and non-enabled balance totals and state counts in each group header.
- [x] 2.2 Add responsive styling and accessible explanations for unavailable, unlimited, and insufficient balances.

## 3. Verification and release

- [x] 3.1 Add server/static UI tests covering finite, unavailable, unlimited, insufficient, and empty categories.
- [x] 3.2 Run sidecar tests, JS syntax checks, strict OpenSpec validation, commit the change, publish only `account-auto-scheduler` to HC2, and verify other containers remain unchanged.
