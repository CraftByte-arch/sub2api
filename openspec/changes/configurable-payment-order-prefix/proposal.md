## Why

Sub2API currently generates payment provider merchant order numbers with a fixed `sub2_` prefix. Operators who run multiple deployments, brands, or merchant accounts may need a recognizable merchant order prefix for reconciliation, provider dashboards, and collision avoidance across systems.

The prefix should be configurable from the existing admin payment settings while preserving the current default behavior for existing deployments.

## What Changes

- Add a payment configuration field for the merchant order number prefix.
- Use the configured prefix when generating new payment order `out_trade_no` values.
- Keep the existing suffix format unchanged: date in `yyyyMMdd` format plus an 8-character random alphanumeric string.
- Expose the prefix in the admin payment settings UI and persist it through the existing settings update flow.
- Validate the prefix so generated merchant order numbers remain compatible with supported payment providers.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `payment`: Payment configuration and order creation support a configurable merchant order number prefix.

## Impact

- Affects backend payment configuration parsing, updating, validation, and order creation.
- Affects the admin payment settings page and related TypeScript API types.
- Does not migrate or rewrite historical `out_trade_no` values.
- Does not change webhook lookup semantics; callbacks continue to locate orders by the full stored `out_trade_no`.
