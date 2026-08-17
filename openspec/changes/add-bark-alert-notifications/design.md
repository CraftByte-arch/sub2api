## Context

The scheduler persists its state in a versioned JSON store and already has a single encrypted credential key (`AUTO_SCHEDULER_CREDENTIAL_KEY`) used for upstream sessions and direct probes. Account checks update persisted account snapshots, while the upstream manager periodically refreshes balances, remote-key multipliers, and physical group bindings. The existing administrator UI is served by the sidecar and already exposes account and group configuration dialogs.

The requested alerts are transition-driven rather than polling-driven: a low-balance condition, zero usable capacity, or a multiplier change must produce one event and remain quiet until the corresponding recovery or next change. Notification delivery must not block probing, balance synchronization, or binding protection.

## Goals / Non-Goals

**Goals:**

- Add one administrator-configured Bark destination with encrypted-at-rest secrets.
- Send Bark end-to-end encrypted payloads using the documented AES-128-CBC format (`ciphertext` plus `iv`); HTTPS and Bark Basic Auth remain additional transport/access protection.
- Support account-level and group-level balance thresholds with account precedence.
- Persist alert edge state across process restarts and state-file migrations.
- Detect balance, group-capacity, and final-multiplier transitions from the same projections shown by the scheduler UI.
- Provide administrator UI/API to configure Bark, test delivery, and set/clear thresholds.

**Non-Goals:**

- No Sub2API code, schema, or billing changes.
- No multi-destination routing, message templates, or arbitrary third-party webhook support in this change.
- No Bark administration UI, device registration, or push-history browser; the sidecar only sends to a configured device key.
- No automatic retry loop that can repeat an alert while the underlying condition remains active.

## Decisions

### 1. A sidecar notification coordinator owns transition evaluation

Add an `internal/notify` coordinator started from `cmd/server/main.go`. It periodically reads current Sub2API groups/accounts and the upstream manager's persisted projections. This keeps notifications independent of the probe engine and upstream sync loops, so a Bark outage cannot affect scheduling. The interval is configurable with a bounded environment variable and defaults to 30 seconds.

The coordinator evaluates physical API-key group membership, active/schedulable account state, the existing available-balance projection (upstream USD balance divided by the observed group multiplier), final multipliers, and multiplier-protection views. It does not perform upstream HTTP calls itself.

### 2. Persist configuration and edge state in the existing JSON store

Extend `model.State` with optional notification settings, group balance thresholds, and alert-state maps; accept all previous state versions and write the current version after migration. Account thresholds live on `ManagedAccount` so the existing account configuration endpoint can update them without a second account store.

Alert state is keyed by semantic relation:

- `balance:<group-id>:<account-id>` (or `balance:ungrouped:<account-id>` for an account-level threshold with no group), storing whether the relation is currently below threshold.
- `capacity:<group-id>`, storing the last capacity band (`zero`, `one`, `many`). Recovery is only the transition to `many` (>1), as requested.
- `multiplier:<group-id>:<account-id>`, storing the last observed final multiplier and protection-triggered flag; the first observation initializes silently.

State is advanced before the network call for a transition and records the last delivery error. This gives at-most-one automatic attempt per persisted transition; a failed delivery is visible in logs/UI and is not retried every poll.

### 3. Reuse the deployment credential key with a separate AEAD domain

Add generic byte-envelope helpers to the existing `upstream.CredentialBox` using a distinct AAD string (`bark-notification/v1`). The notification record stores only the encrypted JSON envelope; device key, AES key, and Bark Basic Auth password are never serialized in plaintext. API responses expose only configured/last-error metadata. If the credential key is absent, notification configuration and test delivery return a clear error while all existing legacy scheduling remains available.

### 4. Fixed AES-128-CBC Bark payload format for this change

The UI labels the required encryption key as a 16-byte key and explains that the same key must be configured in the Bark app. For each push, the coordinator JSON-encodes the Bark parameter object (`title`, `body`, `level`, and a scheduler group), applies PKCS#7 padding, encrypts with AES-128-CBC and a cryptographically random 16-byte printable IV, base64-encodes the ciphertext, and posts `ciphertext` and `iv` to the configured device endpoint. The existing Bark Basic Auth username/password is sent as an HTTP Authorization header. Supporting other Bark algorithms is intentionally deferred until needed.

### 5. Transition messages and precedence

- Effective balance threshold: account threshold when set, otherwise the threshold of each physical/logical group relation; unset means no balance alert.
- Below threshold: send one alert with group, account, current available balance, and threshold. On a later safe observation, send one recovery message and clear the active edge.
- Group capacity: count API-key accounts that are active and schedulable and physically bound to the group. Send on entering zero; do not recover at one; recover only when count becomes greater than one.
- Multiplier: for each bound group-account relation, send only after the initialized previous final multiplier changes. Include old/new values and whether the current relation is under multiplier protection.

All messages are generated from names and numeric state only. API keys, cookies, upstream login tokens, Bark device keys, encryption keys, and Basic Auth passwords are excluded.

### 6. Administrator surface

Add admin-authenticated endpoints for notification settings/test and threshold CRUD. Extend the existing account config modal with an optional balance threshold and group headers with a balance-alert threshold action. Add a compact notification settings dialog with masked secret fields, configuration status, encryption instructions, and a test-send button. The existing overview response includes effective threshold metadata so the UI can show account-over-group precedence without exposing secrets.

## Risks / Trade-offs

- [Bark endpoint unavailable] → Log the bounded error and keep the persisted edge active; do not block scheduler work or poll-spam repeated alerts.
- [Process crash between state transition and delivery] → Advance the edge state before delivery, making duplicates less likely at the cost of a possible missed alert; expose test delivery and last delivery error for operator recovery.
- [Stale/unavailable balance or multiplier] → Treat the value as not observable and do not create a false low-balance or multiplier-change event.
- [Multiple groups per account] → Evaluate each physical group relation independently and include the group in the message; account-level threshold overrides each group's threshold.
- [Existing state files] → Add optional fields and tolerant zero-value initialization; migrate only the version marker and never rewrite secrets in plaintext.

## Migration Plan

1. Deploy the sidecar with the new binary; old state JSON loads with empty notification settings and no alert edges.
2. Configure `AUTO_SCHEDULER_CREDENTIAL_KEY` (already required for upstream features), then enter Bark endpoint, device key, Basic Auth credentials, and the 16-byte encryption key in the admin dialog.
3. Save thresholds and use the test button before enabling notifications.
4. Rollback is the previous sidecar image; unknown new JSON fields are ignored by older binaries, and no Sub2API data is changed.

## Open Questions

- None for the single-device, AES-128-CBC scope. Multi-device routing and additional Bark algorithms can be added as a follow-up without changing alert edge semantics.
