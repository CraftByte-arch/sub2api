## Context

`account-auto-scheduler` is an independent Go sidecar embedded as one administrator-only custom menu in Sub2API. It already validates the browser's administrator JWT against the existing Sub2API instance, reads redacted account metadata over administrator HTTP APIs, persists scheduler state in one atomically replaced JSON file, and serves a compact static HTML/CSS/JavaScript console. The original Sub2API `backend/` and `frontend/` are intentionally out of scope.

The redacted account response retains non-secret credential fields such as `base_url`, so local API-key accounts can be grouped by upstream site without exporting their keys. In contrast, upstream Sub2API and NewAPI installations expose key quota, use, group, and multiplier information only through authenticated user APIs, and their deployments vary by version, reverse-proxy prefix, CAPTCHA, and two-factor settings. Reusable access material therefore introduces a new security boundary inside the sidecar.

## Goals / Non-Goals

**Goals:**

- Add a lazy-loaded `Upstreams` operational view without registering another Sub2API menu item.
- Discover upstream candidates from local API-key account base URLs and support manually supplied upstream URLs.
- Detect Sub2API and NewAPI deployments without sending credentials and permit an explicit type override.
- Support multiple independently named login identities per upstream through password login, access token, or cookie/session input.
- Encrypt all reusable tokens and cookies at rest, keep passwords ephemeral, and return only redacted data to the browser.
- Synchronize remote keys, quota, use, group, and multiplier data through version-tolerant adapters.
- Correlate remote keys with local API-key accounts manually or through a deliberately invoked, step-up-protected key match.
- Keep upstream synchronization isolated from the existing account probe scheduler and its 10-second overview polling.

**Non-Goals:**

- Modifying Sub2API backend or frontend source, database tables, or existing containers.
- Bypassing CAPTCHA, interactive OAuth, passkeys, or two-factor authentication.
- Writing remote multipliers or quota values back to local Sub2API accounts.
- Changing, creating, deleting, or revealing upstream keys.
- Guaranteeing support for arbitrary NewAPI forks whose contracts differ from both detected families.
- Persisting plaintext local or remote API keys, passwords, Authorization headers, or Cookie headers.

## Decisions

### 1. Add a separate upstream manager and adapter boundary

A new `internal/upstream` package will own discovery, connection identities, encrypted authentication material, synchronization, and binding. It will depend on the existing store and a narrow local-Sub2API client interface. A family adapter will expose detection, authentication verification, and key synchronization operations for `sub2api` and `newapi`.

This keeps slow or unreliable upstream calls out of the health-probe engine and makes adapter response parsing independently testable. Folding this behavior into `engine.Engine` was rejected because login credentials and periodic metadata synchronization have different lifecycles, concurrency, and failure semantics from account health checks.

### 2. Derive stable upstream candidates from normalized deployment roots

The local account DTO will retain the already-redacted `credentials.base_url`. A normalizer will require an absolute HTTP(S) URL, remove credentials/query/fragment, remove a trailing OpenAI-compatible `/v1` or panel `/api/v1` suffix while preserving any reverse-proxy deployment prefix, normalize default ports and trailing slashes, and derive a stable opaque ID from SHA-256 of the normalized URL.

`GET /api/upstreams` will merge current local API-key candidates with persisted upstream records. A candidate is persisted only after detection, type override, identity creation, or manual URL creation. Persisted records remain visible when no local account currently references them so that identities and bindings are not silently lost.

### 3. Probe type anonymously before accepting credentials

Detection will make bounded, credential-free requests to the normalized root:

- Sub2API: `GET <root>/api/v1/settings/public`
- NewAPI: `GET <root>/api/status`, with `GET <root>/api/user/groups` used only as a secondary family signal

Detection records evidence and one of `sub2api`, `newapi`, or `unknown`. It never fans a submitted password, token, or cookie across guessed endpoints. The connection dialog preselects a confident result but allows the administrator to set a durable override. Authentication uses only the selected adapter.

### 4. Use same-origin, bounded HTTP clients

Upstream clients will use standard-library transports with connect/TLS/header timeouts, total request deadlines, response-size limits, and a descriptive sidecar user agent. Redirects are limited and accepted only when scheme and host remain equal to the configured root; credentials are never forwarded to a different origin. Private addresses are not categorically blocked because administrators may intentionally operate internal upstreams, but only authenticated administrators can configure a target.

Adapter errors are converted to stable codes and short sanitized messages. Raw response bodies, request headers, tokens, cookies, passwords, and complete key values are never logged or returned.

### 5. Encrypt durable authentication material with a dedicated key

`AUTO_SCHEDULER_CREDENTIAL_KEY` will accept a base64- or hex-encoded 32-byte key. AES-256-GCM will encrypt a versioned authentication-material JSON envelope using a random nonce and associated data containing the upstream and identity IDs. A derived HMAC-SHA-256 key will be used only for in-memory key comparison and optional non-reversible fingerprints.

Password login may produce an access token, refresh token, or session cookie; only those reusable results are encrypted and the password is discarded before the handler returns. Token and cookie modes encrypt the supplied material only after adapter verification succeeds. Decryption failure marks the identity invalid without exposing ciphertext details.

When the environment key is absent or invalid, the sidecar and existing scheduler still start. Session and upstream responses report `credentials_enabled: false`; connection, synchronization requiring stored credentials, and automatic matching return a service-unavailable error with a configuration action. Existing unencrypted scheduler state remains readable.

### 6. Migrate the state file without losing scheduler data

State version 2 will retain the existing managed-account map and add upstream records, identities, synchronized key snapshots, and bindings. Store loading will migrate version 1 in memory by initializing the new maps, then write version 2 on the next mutation. Atomic file replacement, `0600` permissions, deep cloning, and store locking remain the persistence mechanism.

Identity responses use dedicated public view structs that omit encrypted material. Remote key snapshots retain only remote stable ID, display name, masked key, status, group, quota/use, multiplier metadata, timestamps, and an optional HMAC fingerprint. Local bindings reference numeric local account IDs and are validated against the upstream's current local candidates.

### 7. Authenticate and synchronize by site family

The Sub2API adapter uses `/api/v1/auth/login` for password login, `/api/v1/auth/me` for verification, paginated `/api/v1/keys` for keys, and `/api/v1/groups/available` plus `/api/v1/groups/rates` for effective group multipliers. It handles the standard response envelope and reports CAPTCHA or TOTP-required responses without attempting a bypass.

The NewAPI adapter uses `/api/user/login`, verifies through `/api/user/self`, reads paginated `/api/token/`, group ratios from `/api/user/self/groups`, and recent usage records from `/api/log/self`. Password login captures the returned access token and same-origin refresh cookie; an expired access token may be refreshed through `/api/user/auth/refresh`. Both current masked-key responses and older full-key responses are accepted.

For a NewAPI key in a fixed group, the synchronized multiplier comes from the returned group ratio. The `auto` group is labeled dynamic; the adapter searches recent consumption logs for that token name or ID and records the latest numeric `other.group_ratio` with its observation time. Lack of a matching log is represented as dynamic with no observed value, not as zero.

### 8. Make bindings explicit and automatic matching step-up protected

Administrators can save a batch of remote-key-to-local-account bindings for one upstream. The server rejects nonexistent, non-API-key, or differently normalized local accounts. Bindings survive synchronization by remote identity ID and remote key ID, while removed keys are shown as stale until the administrator clears the binding.

Automatic matching is a separate button and API action. The sidecar forwards the current administrator Bearer JWT to `GET /api/v1/admin/accounts/data?ids=<candidate-ids>&include_proxies=false`, so the existing Sub2API step-up policy remains authoritative. It extracts local API keys only in server memory, obtains full remote keys only where the selected adapter/version exposes them, compares HMAC fingerprints using constant-time equality, records unique matches, and immediately releases plaintext values. Ambiguous and unavailable keys are reported without partial secret data. The machine Admin API key is never used for this export.

### 9. Synchronize lazily and at low frequency

The browser will not fetch upstream data during initial group/account rendering. Selecting `Upstreams` triggers the first list request; a manual refresh starts bounded synchronization and reports per-identity results. A manager loop refreshes connected identities at `AUTO_SCHEDULER_UPSTREAM_SYNC_SECONDS` (default 600 seconds, zero disables) with small bounded concurrency. Upstream-origin synchronization remains independent from the existing 10-second overview poll; only the local sidecar list is reloaded while the tab is visible.

One identity failure updates only that identity's status and last error while preserving its previous successful snapshot and timestamp. Process shutdown cancels in-flight calls through the existing root context.

### 10. Extend the existing visual system instead of creating a second console style

The page gains an accessible secondary tablist: `Groups & Accounts` and `Upstreams`. The upstream view uses the current neutral surfaces, semantic status colors, system font, 4/8px spacing rhythm, 8px radius, restrained shadow, and light/dark tokens. Each upstream is one expandable operational row rather than a nested decorative card.

Connection is a native dialog with a visible site-type selector, segmented authentication method, progressively disclosed fields, password visibility control, inline errors, disabled/loading states, and focus restoration. On narrow screens, key columns reflow into labeled vertical rows without horizontal page overflow. Tab, disclosure, dialog, and status controls expose selected/expanded/live states; hover tooltips are supplementary rather than required for essential information; reduced-motion preferences disable transitions.

### 11. Support both current and legacy NewAPI authentication contracts

NewAPI password login will accept both the current authentication bundle (`data.user`, access token, refresh cookie) and the legacy response where `data` is the user record and the durable session is returned only as a cookie. The adapter will always retain the numeric user ID when available and send it as `New-Api-User`, because legacy NewAPI middleware requires that header even when the session cookie is valid.

Token and Cookie/session modes accept an optional NewAPI user ID. It is stored only inside the encrypted authentication material and is never exposed in the public identity view. A rejected legacy session without a user ID returns an actionable error directing the administrator to enter the ID instead of reporting a generic expiration.

### 12. Normalize recharge rates and derive final multipliers at presentation time

Each persisted upstream may contain one positive finite canonical recharge rate in `CNY / USD`, together with the administrator's selected input convention and original positive value. `1 CNY = N USD` is normalized to `1 / N CNY/USD`; `N CNY = 1 USD` is already `N CNY/USD`. Clearing the setting removes all three values.

Synchronized remote keys continue to store only their upstream group multiplier. Public views derive `final_multiplier = recharge_rate_cny_per_usd × group_multiplier` so a recharge-rate edit takes effect immediately without rewriting key snapshots. If either operand is unknown, the final multiplier remains unknown. The upstream row shows the canonical recharge rate and the key row labels group, recharge, and final values explicitly.

### 13. Reconcile local-account correlation without deleting upstream state

Every authoritative upstream-list read obtains the current redacted Sub2API account list. Before producing public views, the manager reconciles persisted `LocalAccountID` bindings against current API-key accounts and their normalized upstream roots. A binding is cleared when the local account no longer exists, is no longer API-key based, has no valid base URL, or now belongs to another upstream. The same reconciliation runs at startup and on the configured periodic cycle so cleanup does not depend on an administrator opening the page.

This reconciliation never deletes the persisted upstream, identity, encrypted credential, recharge rate, or last-good remote-key snapshot. A persisted upstream with no current local accounts remains visible with an empty local-account list. Automatically derived, non-persisted candidates naturally disappear when their last local account disappears.

The browser preserves lazy initial loading: it makes no upstream request until the tab is first opened. Once opened, each tab activation reloads `/api/upstreams`, and the existing 10-second page timer reloads the list only while the upstream tab is visible. These list reloads perform no upstream-origin HTTP synchronization.

## Risks / Trade-offs

- [Upstream forks change endpoint or response formats] -> Keep adapters version-tolerant, cap parsing, preserve the last good snapshot, surface an actionable unsupported-contract state, and isolate fixtures in adapter tests.
- [A configured target can reach an internal service] -> Restrict configuration to validated administrators, accept only HTTP(S) deployment roots, prevent credential-bearing cross-origin redirects, and never expose arbitrary response bodies. Private upstreams remain intentionally supported.
- [Encryption-key rotation makes existing identities unreadable] -> Mark affected identities invalid and allow reconnection; document that rotation requires reconnecting identities in the first version.
- [Password login encounters CAPTCHA, passkey, OAuth-only login, or TOTP] -> Stop with a distinct status and direct the administrator to complete login externally, then paste an access token or session material.
- [Remote APIs return only masked keys] -> Keep manual binding available and report those keys as unavailable for automatic matching; never weaken matching to prefixes or suffixes.
- [Remote key names are not unique, especially for NewAPI log lookup] -> Prefer stable remote IDs and token IDs when present; treat multiple candidate logs or matches as ambiguous rather than guessing.
- [Stale snapshots may be mistaken for current data] -> Display last-success and last-attempt timestamps and keep error state visible alongside retained values.
- [State file grows with many identities and keys] -> Persist only the latest normalized snapshot, impose response/key count limits, and avoid storing log history.
- [Account deletion accidentally erases reusable upstream credentials] -> Clear only the local binding; retain the persisted upstream and require an explicit administrator action for destructive upstream deletion.

## Migration Plan

1. Deploy the new sidecar image with `AUTO_SCHEDULER_CREDENTIAL_KEY` and the optional synchronization interval; do not recreate Sub2API, its database, proxy, or Nginx UI containers.
2. On startup, load version 1 state into the version 2 model without writing until the first mutation, then continue existing probe scheduling.
3. Verify the existing `Groups & Accounts` tab before opening `Upstreams`; detection and connection are opt-in and create no outbound credentialed requests until an administrator submits the dialog.
4. Roll back by replacing only the sidecar image. A previous binary cannot read a version 2 state file, so preserve a pre-deployment volume backup or restore the version 1 state file during rollback.

## Open Questions

None for the first implementation. Additional upstream families and writing multipliers back to local accounts require separate changes.
