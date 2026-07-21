# Usage Card Redeem Generation Fix Design

**Date:** 2026-07-21

## Context

The admin redeem-code API accepts `usage_card_plan_id`, but
`adminServiceImpl.GenerateRedeemCodes` drops it when constructing persisted
`RedeemCode` records. The user redemption path correctly rejects those malformed
records because they cannot identify which usage-card plan to issue.

This behavior was reintroduced by merge commit `b16749151`, which kept the
refactored `admin_user.go` implementation from `main` but lost the usage-card
branch that existed in the other parent.

## Scope

Fix future usage-card redeem-code generation only. Do not alter existing
production redeem-code rows, including the currently unused malformed row.

## Design

Restore the pre-regression behavior inside `GenerateRedeemCodes`:

- Require a positive `UsageCardPlanID` for `usage_card` requests.
- Reject `GroupID` and `ValidityDays` for the usage-card type.
- Resolve the referenced usage-card plan through the existing admin service
  repository dependency before creating any codes.
- Derive each code's value from the resolved plan rather than trusting the
  request value.
- Persist both `UsageCardPlanID` and the loaded plan relationship on every
  generated code.

Other redeem-code types retain their current behavior.

## Testing

Add service-level regression coverage that first demonstrates the current
failure: a usage-card generation request with a valid plan produces a code whose
plan ID is missing. After the implementation change, require the generated code
to contain the selected plan ID and plan-derived value. Also cover invalid
usage-card requests so malformed rows cannot be created again.

Run the focused service tests, the repository's MDC1 release validation, and
the broader backend/frontend checks appropriate to the touched code before
deployment. Release the committed snapshot through the inactive MDC1 slot and
verify health plus `homepage_variant=aixw` before switching traffic.
