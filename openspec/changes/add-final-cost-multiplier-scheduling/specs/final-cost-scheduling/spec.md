## ADDED Requirements

### Requirement: Final cost multiplier is independent from account billing

The system SHALL store an optional `final_cost_multiplier` scheduling value independently from `accounts.rate_multiplier`, and SHALL NOT use the new value to calculate account quota consumption, account billing statistics, user charges, or historical usage log snapshots.

#### Scenario: Final cost value is written

- **WHEN** the sidecar has a unique valid API Key binding, recharge rate, and upstream key multiplier
- **THEN** it writes the calculated final cost multiplier to `extra.final_cost_multiplier` without changing `accounts.rate_multiplier`

#### Scenario: Final cost value is unavailable

- **WHEN** the account is unbound, ambiguously bound, missing a recharge rate, or missing a valid upstream key multiplier
- **THEN** the sidecar clears `extra.final_cost_multiplier` and the account retains its existing billing multiplier and scheduling behavior

### Requirement: Final cost multiplier calculation

The system SHALL calculate `final_cost_multiplier` as the canonical recharge rate in CNY per USD multiplied by the bound upstream key/group multiplier, and SHALL accept zero as a valid result while rejecting non-finite values.

#### Scenario: Canonical recharge conversion

- **WHEN** an upstream is configured as `1 CNY = 5 USD` and the bound upstream key multiplier is `0.8`
- **THEN** the stored final cost multiplier is `0.16`

#### Scenario: Invalid calculation

- **WHEN** either input is missing, non-finite, or the multiplication is non-finite
- **THEN** no numeric final cost multiplier is stored

### Requirement: Profit scheduling prefers final cost when configured

The system SHALL use a valid `final_cost_multiplier` as the account cost signal for OpenAI API Key profit-oriented scheduling and profit admission, and SHALL preserve the existing signal and ordering when the field is absent or invalid.

#### Scenario: Final cost overrides probe ordering

- **WHEN** two eligible OpenAI API Key accounts have different valid final cost multipliers and their probe multipliers would rank them in the opposite order
- **THEN** profit-oriented scheduling ranks the account with the lower final cost multiplier first

#### Scenario: Missing final cost falls back

- **WHEN** an eligible account has no valid `final_cost_multiplier`
- **THEN** OpenAI scheduling uses the existing fresh probe/legacy ordering and the profit gate uses the existing `accounts.rate_multiplier` behavior

#### Scenario: Zero final cost is honored

- **WHEN** an eligible account has `final_cost_multiplier = 0`
- **THEN** the account is treated as a valid zero-cost candidate rather than as missing data

### Requirement: Existing account behavior remains unchanged by default

The system SHALL keep all existing account billing, quota, probe, load, priority, sticky-session, and failover behavior unchanged for accounts without a valid final cost multiplier.

#### Scenario: Legacy account without the field

- **WHEN** an existing account has no `extra.final_cost_multiplier`
- **THEN** requests, accounting, quota updates, probe display, and scheduling produce the same result as before this feature

#### Scenario: Standalone Sub2API deployment

- **WHEN** Sub2API is deployed without the sidecar and no account contains a valid `extra.final_cost_multiplier`
- **THEN** the service has no external dependency on the sidecar and all scheduling, admission, billing, quota, and usage-log behavior follows the original logic

#### Scenario: Clearing the field restores legacy behavior

- **WHEN** `extra.final_cost_multiplier` is cleared after previously being set
- **THEN** subsequent scheduling and profit admission immediately use the original fallback signals after normal cache refresh
