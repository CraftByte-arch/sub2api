## 1. State, Configuration, and Cryptography

- [x] 1.1 Add upstream, identity, remote-key, binding, and redacted account base-URL models with normalization and validation helpers.
- [x] 1.2 Upgrade the JSON store to state version 2 with version 1 migration, deep cloning, upstream CRUD operations, and migration tests.
- [x] 1.3 Implement AES-256-GCM credential envelopes and derived HMAC fingerprints with round-trip, tamper, associated-data, and disabled-key tests.
- [x] 1.4 Add optional credential-key and upstream synchronization interval configuration without changing existing scheduler startup requirements.

## 2. Upstream HTTP Adapters

- [x] 2.1 Implement bounded same-origin HTTP behavior, anonymous Sub2API/NewAPI detection, sanitized adapter errors, and URL normalization tests.
- [x] 2.2 Implement Sub2API password/token/session verification and paginated key, quota, usage, group, and effective-rate synchronization.
- [x] 2.3 Implement NewAPI password/token/session verification, refresh support, paginated token synchronization, fixed group ratios, and dynamic `auto` log observations.
- [x] 2.4 Add adapter fixtures for success, masked keys, CAPTCHA, 2FA, expiry, malformed responses, response limits, and cross-origin redirects.

## 3. Manager and Administrator APIs

- [x] 3.1 Implement upstream candidate aggregation, persisted manual upstreams, public redacted views, and local-account correlation.
- [x] 3.2 Implement multi-identity connection, reconnection, deletion, manual synchronization, and isolated periodic refresh with last-good snapshot retention.
- [x] 3.3 Implement validated batch manual bindings and stale-binding representation.
- [x] 3.4 Add current-admin-JWT account export support and explicit constant-time automatic key matching without machine-key fallback.
- [x] 3.5 Add administrator-protected upstream list, create/detect/type, identity, sync, binding, and automatic-match HTTP routes with bounded request decoding.
- [x] 3.6 Extend server and manager tests for authorization, disabled encryption, redaction, partial sync failure, bindings, and step-up error propagation.

## 4. Administrator Interface

- [x] 4.1 Add an accessible `Groups & Accounts` / `Upstreams` secondary tab shell while preserving the current page and its state.
- [x] 4.2 Implement lazy upstream loading, refresh, search, expandable rows, local-account summaries, identities, synchronized key metadata, and complete loading/empty/error states.
- [x] 4.3 Implement the progressive connection dialog for automatic/manual site type and password/token/cookie modes, including password visibility and actionable challenge states.
- [x] 4.4 Implement manual binding and explicit automatic-match interactions with disabled/loading feedback, step-up guidance, authoritative refresh, and focus restoration.
- [x] 4.5 Finish light/dark styling, 375px responsive reflow, visible focus, non-color status labels, stable control dimensions, tooltips, and reduced-motion behavior.

## 5. Documentation and Deployment Configuration

- [x] 5.1 Document supported upstream contracts, security behavior, encryption-key generation/rotation, synchronization timing, limitations, and rollback requirements.
- [x] 5.2 Add example and hc2 environment wiring for the credential key and optional periodic synchronization without altering other containers.

## 6. Verification

- [x] 6.1 Run formatting, all Go tests, race tests, vet, and JavaScript syntax validation and fix every regression.
- [x] 6.2 Run strict OpenSpec validation and reconcile the implementation with every requirement and task.
- [x] 6.3 Verify the embedded page in a browser at desktop and 375px in light/dark modes, exercise dialogs and keyboard focus, and confirm a clean console and no overlap.

## 7. Legacy NewAPI and Recharge Multipliers

- [x] 7.1 Accept legacy direct-user NewAPI login responses, carry `New-Api-User` in encrypted material, expose an optional manual user-ID field, and add adapter fixtures for both successful and missing-ID flows.
- [x] 7.2 Persist and validate upstream recharge-rate configuration, add an administrator-protected update API, derive final key multipliers in public views, and cover cloning, manager, and handler behavior.
- [x] 7.3 Add an accessible recharge-rate dialog with both input conventions, canonical preview, clear action, explicit group/recharge/final multiplier presentation, and responsive styling.
- [x] 7.4 Update documentation and complete formatting, Go/JavaScript/OpenSpec tests, hc2 sidecar-only deployment, and desktop/mobile browser verification.
- [x] 7.5 Classify explicit NewAPI credential rejection, prevent administrator-host password autofill in the upstream dialog, add liliapi-compatible fixtures and documentation, and deploy only the hc2 sidecar.
- [x] 7.6 Project explicitly bound upstream final multipliers into the groups-and-accounts overview, remove billing-probe multiplier presentation there, and cover the mapping, UI, and API behavior.

## 8. Local Account Reconciliation and Live List Refresh

- [x] 8.1 Reconcile persisted remote-key bindings against current local API-key accounts on list/start/periodic paths, clearing only missing or moved local bindings while retaining upstream state.
- [x] 8.2 Refresh the upstream list on every tab activation and during the existing visible-page poll only while the upstream tab is active, without starting upstream synchronization.
- [x] 8.3 Add manager and JavaScript regression coverage, run Go tests/vet and JavaScript syntax checks, and verify the final diff.
