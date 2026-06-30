# Configurable Payment Order Prefix Verify Report

Change: configurable-payment-order-prefix
Branch: feature/20260624/configurable-payment-order-prefix
Date: 2026-06-30

## Result

PASS. Verification was rerun with Comet scripts available and direct project commands.

## Checks

- OpenSpec tasks: all checklist items in `openspec/changes/configurable-payment-order-prefix/tasks.md` are complete.
- Backend focused tests:
  `cd backend && go test ./internal/service ./internal/handler/admin -run 'Payment|Setting|MerchantOrderPrefix|OutTradeNo|GenerateOutTradeNo' -count=1`
  Result: PASS.
- Frontend SettingsView tests:
  `cd frontend && npx vitest run SettingsView`
  Result: PASS. Existing jsdom/router-link warnings were printed; no test failures.
- Frontend type check:
  `cd frontend && npm run typecheck`
  Result: PASS.
- OpenSpec validation:
  `openspec validate configurable-payment-order-prefix`
  Result: `Change 'configurable-payment-order-prefix' is valid`.

## Fresh Verification Evidence

- `openspec validate configurable-payment-order-prefix`
  Result: PASS.
- `cd backend && go test ./internal/service ./internal/handler/admin -run 'Payment|Setting|MerchantOrderPrefix|OutTradeNo|GenerateOutTradeNo' -count=1`
  Result: PASS (`internal/service`, `internal/handler/admin`).
- `cd frontend && npx vitest run SettingsView`
  Result: PASS, 1 test file passed, 21 tests passed. Existing jsdom/router-link warnings were printed; no test failures.
- `cd frontend && npm run typecheck`
  Result: PASS (`vue-tsc --noEmit`).

## Review Follow-Up

A backend review found that prefix-only payment config updates could erase unrelated payment settings. This was fixed in commit `62a4bd30` by making `PaymentConfigService.UpdatePaymentConfig` write only explicitly provided fields and by adding service and handler regression tests.

## Scope Verified

- Merchant order prefix setting defaults to `sub2_` and validates allowed characters/length.
- Admin integrated settings expose `payment_merchant_order_prefix` for GET/PUT.
- Admin payment config supports `merchant_order_prefix` via existing service DTO passthrough.
- New payment orders use the configured prefix while preserving date plus 8 random alphanumeric suffix and uniqueness retry.
- Legacy `sub2_<id>` notification fallback remains tied to the default legacy prefix.
