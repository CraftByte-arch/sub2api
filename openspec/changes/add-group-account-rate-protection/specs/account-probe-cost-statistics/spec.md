## ADDED Requirements

### Requirement: Probe usage capture
The sidecar SHALL normalize usage returned by supported streaming account checks into input, output, cache, model, and observation fields when those values are present.

#### Scenario: Direct OpenAI or Anthropic check returns usage
- **WHEN** a terminal streaming event contains token usage
- **THEN** the check result SHALL retain the normalized token counts and model used for the probe

#### Scenario: Check has no usage event
- **WHEN** a check completes without usage information
- **THEN** the sidecar SHALL retain the normal success/failure result but SHALL mark monetary probe cost unavailable for that check

### Requirement: One-times-rate probe cost estimation
The sidecar SHALL resolve model pricing through the existing administrator model-pricing API and calculate probe cost with a multiplier of exactly `1.0`.

#### Scenario: Pricing and usage are available
- **WHEN** a check has valid token usage and the model-pricing API returns finite input/output/cache prices
- **THEN** the sidecar SHALL calculate and persist the check cost without applying the account rate or group multiplier

#### Scenario: Pricing is unavailable
- **WHEN** the model is unknown or the pricing API fails
- **THEN** the sidecar SHALL preserve token/request counts, leave the monetary observation unavailable, and SHALL NOT estimate from an arbitrary fallback price

### Requirement: Per-account accumulated detection statistics
The sidecar SHALL persist per-account aggregate detection statistics and expose them in administrator account rows.

#### Scenario: Successful checks accumulate
- **WHEN** multiple checks complete for an account
- **THEN** the aggregate SHALL include detection request count, input/output/cache token totals, total known cost, and the latest usage timestamp

#### Scenario: Statistics are loaded after restart
- **WHEN** the sidecar restarts after previously recorded checks
- **THEN** it SHALL restore the aggregate statistics from state and continue accumulating without resetting them

#### Scenario: Account configuration is deleted
- **WHEN** an administrator deletes an account's detection configuration
- **THEN** the sidecar SHALL delete the associated detection statistics together with the configuration

### Requirement: Administrator-only and non-billing presentation
Probe cost statistics SHALL be returned only through the existing administrator-protected sidecar APIs and SHALL be labeled as detection consumption, not account billing.

#### Scenario: Administrator views a row
- **WHEN** an authenticated administrator opens the groups-and-accounts tab
- **THEN** the account row SHALL show detection requests/tokens and known cost separately from today's account usage and Sub2API quota

#### Scenario: Non-administrator requests statistics
- **WHEN** a caller without a validated administrator session requests overview or configuration data
- **THEN** the sidecar SHALL reject the request before returning detection statistics
