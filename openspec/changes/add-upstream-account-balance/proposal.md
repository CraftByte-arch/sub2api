## Why

The upstream list currently shows synchronized API-key quota and usage but not the actual account balance behind each authenticated upstream identity. Administrators need the authoritative remaining account balance in the same operational view, especially because one upstream can contain multiple independently funded login identities.

## What Changes

- Read the authenticated user's actual balance during Sub2API and NewAPI connection and synchronization using their existing account-profile HTTP APIs.
- Store a normalized, non-secret balance snapshot on each upstream login identity, including amount, currency/unit, source, successful observation time, and freshness state.
- Convert NewAPI internal quota to USD only when the upstream publishes a valid `quota_per_unit`; otherwise expose a clearly labeled raw quota value instead of guessing a monetary amount.
- Preserve the last successful balance when a later synchronization fails, while marking it stale and keeping the failure isolated to that identity.
- Show per-identity balances and a concise upstream-level balance summary in the existing Upstreams tab, with synchronization refreshing both keys and balances.
- Project each fresh USD identity balance onto every explicitly bound local API-key account as Sub2API remaining account quota: `identity balance ÷ bound key group multiplier`, without dividing the site balance among multiple local accounts.
- On each new authoritative balance observation, reset the local account's `quota_used` to zero and set `quota_limit` directly to the derived remaining balance, while representing an authoritative zero balance as immediately exhausted so Sub2API removes the account from scheduling until a later positive balance refresh.
- Keep balance data administrator-only and avoid adding browser-direct upstream requests, new credentials, or changes to the original Sub2API source and database.

## Capabilities

### New Capabilities

- `upstream-account-balance`: Authenticated, identity-scoped upstream account balance collection, persistence, freshness semantics, and administrator presentation.

### Modified Capabilities

None.

## Impact

- Affects only `account-auto-scheduler` models, synchronization/reconciliation, its existing Sub2API administrator HTTP client, tests, documentation, and sidecar deployment.
- Reuses existing encrypted upstream sessions and existing manual/periodic synchronization; no new secret is sent to the browser or stored in plaintext.
- Does not modify `backend/`, `frontend/`, the Sub2API schema, other containers, or upstream account configuration; quota projection reuses the existing administrator Extra merge endpoint.
