## Why

The scheduler can show only the account metadata already exposed by Sub2API, while many upstream Sub2API and NewAPI sites expose the authoritative key quota, usage, and billing multiplier only after user authentication. Administrators need one secure operational view for connecting those upstreams and correlating their keys with local API-key accounts without modifying the original Sub2API application.

## What Changes

- Add a second-level `Upstreams` tab inside the existing account auto-scheduler administrator page, alongside the current group and account view.
- Group local API-key accounts by normalized upstream base URL and allow each upstream row to expand into its associated local accounts and synchronized upstream keys.
- Detect Sub2API and NewAPI sites without credentials, preselect the detected type, and allow an administrator to override an unknown or incorrect result.
- Support multiple connection identities per upstream using personal access tokens, account/password login, or manually supplied session cookies/tokens for sites with CAPTCHA or two-factor authentication.
- Encrypt reusable upstream tokens and cookies at rest with a dedicated deployment key, discard passwords after login, redact secrets in API responses and logs, and disable only connection features when encryption is not configured.
- Show connection health and actionable states such as connected, expired, CAPTCHA required, two-factor authentication required, network failure, and unknown site type.
- Synchronize upstream key status, group, quota, usage, and multiplier data on demand and on a low-frequency schedule independent of the existing overview polling.
- Allow administrators to bind synchronized upstream keys to local API-key accounts manually, with an explicit sensitive automatic-match action that uses the current administrator step-up export endpoint and keeps plaintext keys only in server memory.
- Treat NewAPI `auto` groups as dynamic-rate groups and show the latest observed request-log multiplier when available; the first version does not write synchronized multipliers back to Sub2API accounts.
- Support legacy NewAPI password sessions whose login response returns the user directly and whose authenticated APIs require the `New-Api-User` header; allow administrators to provide that user ID for manually supplied token or cookie sessions.
- Allow an administrator to configure one upstream recharge rate using either `1 CNY = N USD` or `N CNY = 1 USD`, normalize it to `CNY / USD`, and show `recharge rate × group multiplier` as the final multiplier for each synchronized key.
- Refresh local-account correlation whenever the upstream workspace is activated and while it remains visible, without triggering upstream login or metadata synchronization.
- Automatically clear persisted remote-key bindings whose local API-key account was deleted, changed away from API-key type, or moved to a different normalized upstream, while retaining the persisted upstream, identities, and last-good key snapshots.

## Capabilities

### New Capabilities

- `upstream-connection-management`: Administrator-only upstream discovery, encrypted authentication, key synchronization, local-account correlation, and multiplier/usage presentation within the scheduler sidecar.

### Modified Capabilities

None.

## Impact

- Affects only `account-auto-scheduler`, its local persisted state, administrator-only sidecar APIs, embedded static assets, tests, documentation, and sidecar deployment configuration.
- Continues to consume existing Sub2API administrator HTTP APIs, including the step-up account export route for opt-in automatic key matching; no changes are made under `backend/` or `frontend/`.
- Adds outbound HTTP access from the sidecar to administrator-configured upstream origins and requires a new optional `AUTO_SCHEDULER_CREDENTIAL_KEY` secret to enable reusable upstream connections.
- Existing scheduling, probing, group management, Sub2API, database, reverse proxy, and Nginx UI containers remain backward compatible and independently deployable.
