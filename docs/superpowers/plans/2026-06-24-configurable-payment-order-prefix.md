---
change: configurable-payment-order-prefix
design-doc: docs/superpowers/specs/2026-06-24-configurable-payment-order-prefix-design.md
base-ref: d3dedc05ce2404917621670c33ad1412b754073f
---

# Configurable Payment Order Prefix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an admin-configurable merchant order number prefix for newly created payment orders while keeping `sub2_` as the default.

**Architecture:** Extend the existing payment settings pipeline instead of creating a new configuration store. `PaymentConfigService` owns defaulting and validation, `PaymentService` consumes the loaded config during order allocation, and the admin settings UI exposes the field through the existing payment settings form.

**Tech Stack:** Go service layer and handlers, Ent-backed settings repository, Vue 3 admin settings page, TypeScript API types, Vitest frontend tests, Go unit tests.

---

## File Structure

- Modify `backend/internal/service/payment_config_service.go`: add setting key, config/request fields, normalization and validation helper, parsing, and persistence.
- Modify `backend/internal/service/payment_service.go`: make `generateOutTradeNo` accept a prefix and keep the existing suffix behavior.
- Modify `backend/internal/service/payment_order.go`: pass `PaymentConfig` into allocation and retry uniqueness with the configured prefix.
- Modify `backend/internal/handler/admin/setting_handler.go`: include `payment_merchant_order_prefix` in integrated settings request/response and payment update mapping.
- Modify `backend/internal/handler/dto/settings.go`: expose the field in settings DTOs if the response DTO is declared there.
- Modify `backend/internal/handler/admin/payment_handler.go` only if sanitization or response shaping hides the new config field.
- Modify `frontend/src/api/admin/settings.ts`: add `payment_merchant_order_prefix` to settings types.
- Modify `frontend/src/api/admin/payment.ts`: add `merchant_order_prefix` to admin payment config types.
- Modify `frontend/src/views/admin/SettingsView.vue`: add form default, input field, and save payload mapping.
- Modify `frontend/src/i18n/locales/zh.ts` and `frontend/src/i18n/locales/en.ts`: add label/help strings.
- Modify backend and frontend tests covering config parsing, validation, order generation, DTO mapping, and settings form behavior.

---

### Task 1: Backend Payment Config Field

**Files:**
- Modify: `backend/internal/service/payment_config_service.go`
- Modify: `backend/internal/service/payment_config_service_test.go`

- [x] **Step 1: Write failing tests for default, valid, and invalid prefixes**

Add test coverage in `backend/internal/service/payment_config_service_test.go`:

```go
func TestPaymentConfigMerchantOrderPrefix(t *testing.T) {
	t.Parallel()

	svc := &PaymentConfigService{}

	t.Run("default is sub2 underscore", func(t *testing.T) {
		t.Parallel()
		cfg := svc.parsePaymentConfig(map[string]string{})
		if cfg.MerchantOrderPrefix != "sub2_" {
			t.Fatalf("MerchantOrderPrefix = %q, want %q", cfg.MerchantOrderPrefix, "sub2_")
		}
	})

	t.Run("stored value is trimmed", func(t *testing.T) {
		t.Parallel()
		cfg := svc.parsePaymentConfig(map[string]string{
			SettingMerchantOrderPrefix: " myshop_ ",
		})
		if cfg.MerchantOrderPrefix != "myshop_" {
			t.Fatalf("MerchantOrderPrefix = %q, want %q", cfg.MerchantOrderPrefix, "myshop_")
		}
	})
}

func TestUpdatePaymentConfigMerchantOrderPrefixValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("persists valid prefix", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
		svc := &PaymentConfigService{settingRepo: repo}
		prefix := "shop-01_"

		if err := svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{
			MerchantOrderPrefix: &prefix,
		}); err != nil {
			t.Fatalf("UpdatePaymentConfig returned error: %v", err)
		}
		if repo.values[SettingMerchantOrderPrefix] != "shop-01_" {
			t.Fatalf("stored prefix = %q, want %q", repo.values[SettingMerchantOrderPrefix], "shop-01_")
		}
	})

	t.Run("rejects invalid characters", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
		svc := &PaymentConfigService{settingRepo: repo}
		prefix := "bad.prefix"

		err := svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{
			MerchantOrderPrefix: &prefix,
		})
		if err == nil {
			t.Fatal("expected invalid prefix error")
		}
		if got := repo.values[SettingMerchantOrderPrefix]; got != "" {
			t.Fatalf("stored invalid prefix = %q, want empty", got)
		}
	})

	t.Run("rejects too long prefix", func(t *testing.T) {
		repo := &paymentConfigSettingRepoStub{values: map[string]string{}}
		svc := &PaymentConfigService{settingRepo: repo}
		prefix := "abcdefghijklmnopq"

		err := svc.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{
			MerchantOrderPrefix: &prefix,
		})
		if err == nil {
			t.Fatal("expected too long prefix error")
		}
	})
}
```

- [x] **Step 2: Run tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/service -run 'TestPaymentConfigMerchantOrderPrefix|TestUpdatePaymentConfigMerchantOrderPrefixValidation' -count=1
```

Expected: FAIL because `MerchantOrderPrefix`, `SettingMerchantOrderPrefix`, and `UpdatePaymentConfigRequest.MerchantOrderPrefix` do not exist.

- [x] **Step 3: Implement config field and validation**

In `backend/internal/service/payment_config_service.go`:

```go
const (
	SettingMerchantOrderPrefix = "MERCHANT_ORDER_PREFIX"
	defaultMerchantOrderPrefix = "sub2_"
)

type PaymentConfig struct {
	// existing fields...
	MerchantOrderPrefix string `json:"merchant_order_prefix"`
}

type UpdatePaymentConfigRequest struct {
	// existing fields...
	MerchantOrderPrefix *string `json:"merchant_order_prefix"`
}

func normalizeMerchantOrderPrefix(raw string) (string, error) {
	prefix := strings.TrimSpace(raw)
	if prefix == "" {
		return defaultMerchantOrderPrefix, nil
	}
	if len(prefix) > 16 {
		return "", infraerrors.BadRequest("INVALID_MERCHANT_ORDER_PREFIX", "merchant order prefix must be 1-16 characters")
	}
	for _, r := range prefix {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", infraerrors.BadRequest("INVALID_MERCHANT_ORDER_PREFIX", "merchant order prefix may only contain letters, numbers, underscore, and hyphen")
	}
	return prefix, nil
}
```

Add `SettingMerchantOrderPrefix` to `GetPaymentConfig` keys, parse it in `parsePaymentConfig`, validate it in `UpdatePaymentConfig`, and persist `SettingMerchantOrderPrefix` using the normalized value.

- [x] **Step 4: Run tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/service -run 'TestPaymentConfigMerchantOrderPrefix|TestUpdatePaymentConfigMerchantOrderPrefixValidation|TestParsePaymentConfig|TestUpdatePaymentConfig_PersistsVisibleMethodRouting' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit backend config changes**

```bash
git add backend/internal/service/payment_config_service.go backend/internal/service/payment_config_service_test.go
git commit -m "feat: add payment merchant order prefix config"
```

### Task 2: Order Number Generation

**Files:**
- Modify: `backend/internal/service/payment_service.go`
- Modify: `backend/internal/service/payment_order.go`
- Modify: `backend/internal/service/payment_config_service_test.go` or create focused order generation tests in an existing payment order test file.

- [x] **Step 1: Write failing tests for generator behavior**

Add tests near existing payment service/order tests:

```go
func TestGenerateOutTradeNoUsesConfiguredPrefix(t *testing.T) {
	got := generateOutTradeNo("shop_")
	if !strings.HasPrefix(got, "shop_") {
		t.Fatalf("out_trade_no = %q, want prefix shop_", got)
	}
	if len(got) != len("shop_")+8+8 {
		t.Fatalf("out_trade_no length = %d, want %d", len(got), len("shop_")+16)
	}
}

func TestGenerateOutTradeNoFallsBackToDefaultPrefix(t *testing.T) {
	got := generateOutTradeNo("")
	if !strings.HasPrefix(got, "sub2_") {
		t.Fatalf("out_trade_no = %q, want prefix sub2_", got)
	}
}
```

- [x] **Step 2: Run tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/service -run 'TestGenerateOutTradeNoUsesConfiguredPrefix|TestGenerateOutTradeNoFallsBackToDefaultPrefix' -count=1
```

Expected: FAIL because `generateOutTradeNo` currently has no prefix parameter.

- [x] **Step 3: Implement prefix-aware generation**

In `backend/internal/service/payment_service.go`, replace the fixed `orderIDPrefix` usage with `defaultMerchantOrderPrefix` and a prefix parameter:

```go
func generateOutTradeNo(prefix string) string {
	prefix, err := normalizeMerchantOrderPrefix(prefix)
	if err != nil {
		prefix = defaultMerchantOrderPrefix
	}
	date := time.Now().Format("20060102")
	rnd := generateRandomString(8)
	return prefix + date + rnd
}
```

In `backend/internal/service/payment_order.go`, update creation and allocation:

```go
outTradeNo, err := s.allocateOutTradeNo(ctx, tx, cfg.MerchantOrderPrefix)
```

and:

```go
func (s *PaymentService) allocateOutTradeNo(ctx context.Context, tx *dbent.Tx, prefix string) (string, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		candidate := generateOutTradeNo(prefix)
		// existing uniqueness query...
	}
	// existing exhausted error...
}
```

Keep the existing uniqueness query and retry count unchanged.

- [x] **Step 4: Run tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/service -run 'TestGenerateOutTradeNoUsesConfiguredPrefix|TestGenerateOutTradeNoFallsBackToDefaultPrefix|TestPaymentConfigMerchantOrderPrefix' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit order generation changes**

```bash
git add backend/internal/service/payment_service.go backend/internal/service/payment_order.go backend/internal/service/*test.go
git commit -m "feat: generate payment order numbers with configured prefix"
```

### Task 3: Backend Admin DTO Wiring

**Files:**
- Modify: `backend/internal/handler/admin/setting_handler.go`
- Modify: `backend/internal/handler/dto/settings.go`
- Modify: `backend/internal/handler/admin/payment_handler.go` if needed.
- Modify or add backend handler tests if existing tests cover settings DTO responses.

- [x] **Step 1: Write or update failing DTO tests**

Update existing admin settings/payment handler tests to assert the field exists in responses and update requests map into `service.UpdatePaymentConfigRequest`. Use JSON key names:

```json
{
  "payment_merchant_order_prefix": "shop_"
}
```

and admin payment config key:

```json
{
  "merchant_order_prefix": "shop_"
}
```

- [x] **Step 2: Run targeted handler tests and verify failure**

Run the relevant package tests:

```bash
cd backend && go test ./internal/handler/admin -run 'Payment|Setting' -count=1
```

Expected: FAIL on missing field assertions until DTO wiring is added.

- [x] **Step 3: Wire integrated settings**

In `backend/internal/handler/admin/setting_handler.go`, add:

```go
PaymentMerchantOrderPrefix *string `json:"payment_merchant_order_prefix"`
```

to the update request struct, add response field population from `paymentCfg.MerchantOrderPrefix`, add it to `hasPaymentFields`, and map it into:

```go
MerchantOrderPrefix: req.PaymentMerchantOrderPrefix,
```

In `backend/internal/handler/dto/settings.go`, add:

```go
PaymentMerchantOrderPrefix string `json:"payment_merchant_order_prefix"`
```

where payment settings response fields are declared.

- [x] **Step 4: Wire admin payment config if needed**

Because `PaymentConfig` JSON includes `merchant_order_prefix`, `GET /admin/payment/config` should expose it automatically unless sanitization strips it. If sanitization or DTO wrapping exists, add the field there and to the update request path.

- [x] **Step 5: Run backend handler tests**

Run:

```bash
cd backend && go test ./internal/handler/admin ./internal/service -run 'Payment|Setting|MerchantOrderPrefix|PaymentConfig' -count=1
```

Expected: PASS.

- [x] **Step 6: Commit DTO wiring**

```bash
git add backend/internal/handler/admin/setting_handler.go backend/internal/handler/dto/settings.go backend/internal/handler/admin/*test.go backend/internal/service/*test.go
git commit -m "feat: expose merchant order prefix in admin settings"
```

### Task 4: Frontend Admin Settings UI

**Files:**
- Modify: `frontend/src/api/admin/settings.ts`
- Modify: `frontend/src/api/admin/payment.ts`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Modify: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

- [x] **Step 1: Write failing frontend test updates**

In `frontend/src/views/admin/__tests__/SettingsView.spec.ts`, update mock settings data to include:

```ts
payment_merchant_order_prefix: "shop_",
```

Add an assertion that the field is present after settings load and that save payload includes:

```ts
payment_merchant_order_prefix: "shop_",
```

- [x] **Step 2: Run frontend test and verify failure**

Run:

```bash
cd frontend && npm run test -- SettingsView
```

Expected: FAIL until the form field and types exist.

- [x] **Step 3: Add TypeScript fields**

In `frontend/src/api/admin/settings.ts`, add:

```ts
payment_merchant_order_prefix: string;
```

to `SystemSettings`, and:

```ts
payment_merchant_order_prefix?: string;
```

to `UpdateSettingsRequest`.

In `frontend/src/api/admin/payment.ts`, add:

```ts
merchant_order_prefix: string
```

to `AdminPaymentConfig`, and:

```ts
merchant_order_prefix?: string
```

to `UpdatePaymentConfigRequest`.

- [x] **Step 4: Add SettingsView form default and save payload**

In `frontend/src/views/admin/SettingsView.vue`, add default:

```ts
payment_merchant_order_prefix: "sub2_",
```

and save payload:

```ts
payment_merchant_order_prefix: form.payment_merchant_order_prefix || "sub2_",
```

- [x] **Step 5: Add payment settings input**

Near the existing product name prefix/suffix row, add a compact text input:

```vue
<div>
  <label class="input-label">{{ t("admin.settings.payment.merchantOrderPrefix") }}</label>
  <input
    v-model="form.payment_merchant_order_prefix"
    type="text"
    maxlength="16"
    class="input"
    placeholder="sub2_"
  />
  <p class="mt-0.5 text-xs text-gray-400">
    {{ t("admin.settings.payment.merchantOrderPrefixHint") }}
  </p>
</div>
```

Adjust the surrounding grid columns so the row remains responsive and text does not overlap.

- [x] **Step 6: Add i18n strings**

In `frontend/src/i18n/locales/zh.ts`:

```ts
merchantOrderPrefix: '商户订单号前缀',
merchantOrderPrefixHint: '用于新支付订单的商户订单号前缀，仅支持字母、数字、下划线和短横线。',
```

In `frontend/src/i18n/locales/en.ts`:

```ts
merchantOrderPrefix: 'Merchant order prefix',
merchantOrderPrefixHint: 'Prefix for new payment merchant order numbers. Letters, numbers, underscore, and hyphen only.',
```

- [ ] **Step 7: Run frontend tests/type checks**

Run:

```bash
cd frontend && npm run test -- SettingsView
cd frontend && npm run type-check
```

Expected: PASS.

- [ ] **Step 8: Commit frontend changes**

```bash
git add frontend/src/api/admin/settings.ts frontend/src/api/admin/payment.ts frontend/src/views/admin/SettingsView.vue frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts frontend/src/views/admin/__tests__/SettingsView.spec.ts
git commit -m "feat: add merchant order prefix setting UI"
```

### Task 5: Final Verification And OpenSpec Tasks

**Files:**
- Modify: `openspec/changes/configurable-payment-order-prefix/tasks.md`

- [x] **Step 1: Run backend focused tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler/admin -run 'Payment|Setting|MerchantOrderPrefix|OutTradeNo' -count=1
```

Expected: PASS.

- [x] **Step 2: Run frontend focused checks**

Run:

```bash
cd frontend && npm run test -- SettingsView
cd frontend && npm run type-check
```

Expected: PASS.

- [x] **Step 3: Validate OpenSpec change**

Run:

```bash
openspec validate configurable-payment-order-prefix
```

Expected: `Change 'configurable-payment-order-prefix' is valid`.

- [x] **Step 4: Mark OpenSpec tasks complete**

Update `openspec/changes/configurable-payment-order-prefix/tasks.md` by changing every completed task from `- [ ]` to `- [x]`.

- [x] **Step 5: Commit task completion**

```bash
git add openspec/changes/configurable-payment-order-prefix/tasks.md
git commit -m "chore: complete configurable payment order prefix tasks"
```

## Self-Review

- Spec coverage: default prefix, custom prefix, invalid prefix rejection, and historical callback compatibility are covered by Tasks 1-5.
- Placeholder scan: no unresolved placeholder markers or unspecified implementation steps remain.
- Type consistency: backend uses `MerchantOrderPrefix` / `merchant_order_prefix`; integrated admin settings uses `PaymentMerchantOrderPrefix` / `payment_merchant_order_prefix`.
