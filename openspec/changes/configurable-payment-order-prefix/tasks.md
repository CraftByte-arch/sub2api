## 1. Backend Configuration

- [x] 1.1 Add a merchant order prefix payment setting with default `sub2_`.
- [x] 1.2 Add parsing, update, and validation coverage in `PaymentConfigService`.
- [x] 1.3 Include the field in admin settings and admin payment config request/response DTOs.

## 2. Order Number Generation

- [x] 2.1 Change `out_trade_no` allocation to use the configured prefix.
- [x] 2.2 Preserve existing date plus 8-character random suffix behavior.
- [x] 2.3 Add tests for default prefix, custom prefix, and uniqueness retry behavior.

## 3. Admin UI

- [ ] 3.1 Add the merchant order prefix field to the payment settings page.
- [ ] 3.2 Update frontend API types, form defaults, payload mapping, and i18n strings.
- [ ] 3.3 Add or update frontend tests for loading and saving the new setting.

## 4. Verification

- [ ] 4.1 Run targeted backend payment configuration and order tests.
- [ ] 4.2 Run targeted frontend settings tests or type checks.
- [ ] 4.3 Validate the OpenSpec change artifacts.
