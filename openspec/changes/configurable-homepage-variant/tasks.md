## 1. Runtime Configuration

- [x] 1.1 Add minimal backend environment support for `HOMEPAGE_VARIANT` with normalization to `default` or `aixw`.
- [x] 1.2 Expose the normalized homepage variant through service public settings, DTO public settings, and HTML injection payloads.
- [x] 1.3 Document `HOMEPAGE_VARIANT` in Docker environment examples.

## 2. Frontend Homepage Selection

- [x] 2.1 Add frontend public settings typing and default handling for the homepage variant.
- [x] 2.2 Route `/home` through a thin runtime selector that renders the upstream `HomeView` or custom `AixwHomeView`.
- [x] 2.3 Preserve `/` redirect behavior and unauthenticated access behavior for `/home`.

## 3. Verification

- [x] 3.1 Add backend tests for missing, invalid, `default`, and `aixw` homepage variant values.
- [x] 3.2 Add frontend tests for runtime homepage selection from injected/public settings.
- [x] 3.3 Run focused backend and frontend verification commands.

<!-- review skipped: subagent review tool requires explicit user authorization for delegation -->
