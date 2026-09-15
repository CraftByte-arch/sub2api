## Why

The sidecar account list shows requests, total tokens, and cost but does not expose how effectively each upstream account reuses prompt caches. Administrators need the same cache-hit signal already used by Sub2API usage views to compare account quality without leaving the scheduler page.

## What Changes

- Enrich sidecar-only account usage snapshots with today's input, cache-creation, and cache-read token counts by calling an existing Sub2API administrator API.
- Calculate and expose an account cache hit rate using the existing Sub2API convention: cache-read tokens divided by input, cache-creation, and cache-read prompt tokens.
- Display the rate and token breakdown in the existing compact account usage area, including an unavailable state when enrichment cannot be obtained.
- Keep the base overview available when cache statistics fail and avoid modifying Sub2API production code or database schemas.

## Capabilities

### New Capabilities

- `sidecar-account-cache-hit-rate`: Collect and display a best-effort daily cache hit rate for accounts in the sidecar administrator interface.

### Modified Capabilities

None.

## Impact

- Affects only `account-auto-scheduler` model, Sub2API administrator client, overview payload, static UI, and tests.
- Reuses `GET /api/v1/admin/accounts/:id/stats?days=1`; no new Sub2API endpoint or schema change is introduced.
- Adds bounded parallel administrator API reads with a short in-memory cache so normal overview polling does not repeatedly query every account.
