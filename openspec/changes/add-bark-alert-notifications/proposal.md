## Why

The automatic scheduler already knows when upstream balances, effective multipliers, and group availability change, but administrators currently have to watch the console to notice these transitions. Integrating encrypted Bark notifications will turn those state changes into actionable alerts without modifying Sub2API or sending credentials outside the scheduler's configured notification path.

## What Changes

- Add Bark notification configuration to the automatic scheduler, including server URL, encrypted-push credentials, enablement, and test delivery.
- Allow balance-alert thresholds at group and account scope, with account thresholds taking precedence over group thresholds.
- Emit one notification when an account crosses below its effective available-balance threshold, and one recovery notification after the balance becomes safe again.
- Emit one notification when a group has zero usable accounts, and one recovery notification when usable capacity returns above zero.
- Emit a multiplier-change notification that includes the previous and new final multipliers and whether multiplier protection was triggered.
- Persist notification state so repeated detection cycles do not duplicate alerts and restarts do not reset active alert state.
- Keep Bark credentials encrypted at rest and never include API keys, cookies, tokens, or Bark credentials in notification content or logs.

## Capabilities

### New Capabilities

- `bark-alert-notifications`: Configure encrypted Bark delivery and send deduplicated account, group-capacity, and multiplier-transition alerts.

### Modified Capabilities

<!-- No existing main capability requirements are changed; this is implemented as a sidecar-only capability. -->

## Impact

- Affects only `account-auto-scheduler` configuration, persistence, state-transition evaluation, and administrator UI/API.
- Adds no Sub2API code or database changes.
- Adds an outbound HTTPS dependency from the scheduler to the configured Bark endpoint; notification delivery failures must not stop probing or scheduling.
- Requires a migration-safe local store extension for notification settings and alert state.
