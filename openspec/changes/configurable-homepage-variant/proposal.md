## Why

The project now contains a custom AIXW landing page, while deployments still need a simple way to keep the upstream default homepage without rebuilding the image. A runtime container parameter lets operators choose the homepage variant at startup.

## What Changes

- Add a public runtime homepage variant setting controlled by the `HOMEPAGE_VARIANT` environment variable.
- Support `HOMEPAGE_VARIANT=aixw` to show the custom AIXW homepage.
- Support `HOMEPAGE_VARIANT=default` to show the upstream default homepage.
- Default to `default` when the variable is missing or invalid.
- Expose the selected variant through public settings and the server-side HTML injection payload so the first page load uses the configured homepage.

## Capabilities

### New Capabilities

- `homepage-variant`: Runtime selection of the public homepage variant for container deployments.

### Modified Capabilities

- None.

## Impact

- Backend configuration loading and public settings payloads.
- Frontend public settings type and route/homepage component selection.
- Docker/deploy environment examples documenting `HOMEPAGE_VARIANT`.
- Focused backend/frontend tests for default, AIXW, and invalid values.
