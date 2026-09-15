## Why

Loading the sidecar overview used to request full cache statistics for every account. On HC2, the sidecar's intentionally limited read-only database role cannot read raw `usage_logs`, and no existing readable aggregate contains account-level cache tokens.

## What Changes

- Keep the existing one-request batch endpoint for base today usage.
- Give every account with zero requests today a known-zero cache projection without an extra request.
- Request the existing single-account cache statistics only for accounts that have requests today, with a five-minute sidecar cache and at most four concurrent calls.
- Remove the unusable direct raw-usage-log path; do not change Sub2API code, routes, database grants, schemas, or containers.

## Impact

Only `account-auto-scheduler` code and its HC2 image change. The main service remains untouched.
