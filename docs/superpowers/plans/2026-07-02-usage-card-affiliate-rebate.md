---
change: usage-card-affiliate-rebate
design-doc: docs/superpowers/specs/2026-07-02-usage-card-affiliate-rebate-design.md
base-ref: 8a24d5a7c4b435dea7e38f72aecced711ae7dd1d
---

# Usage Card Affiliate Rebate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Usage card purchases made by invited users accrue inviter rebate from the payment order `pay_amount`.

**Architecture:** Reuse the existing payment fulfillment affiliate path. Add `usage_card` to the rebate base resolver and invoke `applyAffiliateRebateForOrder` from usage card fulfillment after idempotent card issuance and before completion.

**Tech Stack:** Go service layer, Ent test client, existing payment audit idempotency, `go test` with `unit` build tag.

---

## File Structure

- Modify `backend/internal/service/payment_fulfillment.go`: add usage-card base amount support and call affiliate rebate processing from usage card fulfillment.
- Modify `backend/internal/service/payment_fulfillment_test.go`: add usage-card repository test stub, base amount tests, fulfillment rebate tests, skip tests, and idempotency tests.
- Modify `openspec/changes/usage-card-affiliate-rebate/tasks.md`: mark completed OpenSpec tasks as implementation lands.

## Task 1: Rebate Base Amount Uses PayAmount For Usage Cards

**Files:**
- Modify: `backend/internal/service/payment_fulfillment_test.go`
- Modify: `backend/internal/service/payment_fulfillment.go`
- Track: `openspec/changes/usage-card-affiliate-rebate/tasks.md`

- [x] **Step 1: Add failing base amount unit test**

Add this test near the other pure helper tests in `backend/internal/service/payment_fulfillment_test.go`:

```go
func TestAffiliateRebateBaseAmountByOrderType(t *testing.T) {
	t.Parallel()

	balanceOrder := &dbent.PaymentOrder{
		OrderType: payment.OrderTypeBalance,
		Amount:    120,
		PayAmount: 126,
	}
	subscriptionOrder := &dbent.PaymentOrder{
		OrderType: payment.OrderTypeSubscription,
		Amount:    80,
		PayAmount: 84,
	}
	usageCardOrder := &dbent.PaymentOrder{
		OrderType: payment.OrderTypeUsageCard,
		Amount:    500,
		PayAmount: 59.9,
	}
	unsupportedOrder := &dbent.PaymentOrder{
		OrderType: "unsupported",
		Amount:    10,
		PayAmount: 10,
	}

	require.Equal(t, 120.0, affiliateRebateBaseAmount(balanceOrder))
	require.Equal(t, 80.0, affiliateRebateBaseAmount(subscriptionOrder))
	require.Equal(t, 59.9, affiliateRebateBaseAmount(usageCardOrder))
	require.Zero(t, affiliateRebateBaseAmount(unsupportedOrder))
	require.Zero(t, affiliateRebateBaseAmount(nil))
}
```

- [x] **Step 2: Run test to verify it fails**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run TestAffiliateRebateBaseAmountByOrderType -count=1
```

Expected before implementation: FAIL because `usage_card` currently returns `0`.

- [x] **Step 3: Implement the base amount resolver change**

Change `affiliateRebateBaseAmount` in `backend/internal/service/payment_fulfillment.go` to:

```go
func affiliateRebateBaseAmount(o *dbent.PaymentOrder) float64 {
	if o == nil {
		return 0
	}
	switch o.OrderType {
	case payment.OrderTypeBalance, payment.OrderTypeSubscription:
		return o.Amount
	case payment.OrderTypeUsageCard:
		return o.PayAmount
	default:
		return 0
	}
}
```

- [x] **Step 4: Run focused test to verify it passes**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run TestAffiliateRebateBaseAmountByOrderType -count=1
```

Expected after implementation: PASS.

- [x] **Step 5: Mark OpenSpec rebate base tasks**

In `openspec/changes/usage-card-affiliate-rebate/tasks.md`, mark these tasks complete:

```md
- [x] 1.1 Extend affiliate rebate base amount resolution to support `usage_card` payment orders.
- [x] 1.2 Ensure the usage card rebate base uses the order `pay_amount`, not the issued usage card credit amount.
- [x] 1.3 Add focused unit coverage for balance, subscription, usage card, and unsupported order types.
```

- [x] **Step 6: Commit Task 1**

Run:

```bash
git add backend/internal/service/payment_fulfillment.go backend/internal/service/payment_fulfillment_test.go openspec/changes/usage-card-affiliate-rebate/tasks.md
git commit -m "feat: use pay amount for usage card affiliate base"
```

## Task 2: Usage Card Fulfillment Applies Affiliate Rebate

**Files:**
- Modify: `backend/internal/service/payment_fulfillment_test.go`
- Modify: `backend/internal/service/payment_fulfillment.go`
- Track: `openspec/changes/usage-card-affiliate-rebate/tasks.md`

- [x] **Step 1: Add usage card repository test stub**

Add this stub in `backend/internal/service/payment_fulfillment_test.go` near the other fulfillment stubs:

```go
type paymentFulfillmentUsageCardRepoStub struct {
	UsageCardRepository
	plan        *UsageCardPlan
	cardsByOrder map[int64]*UserUsageCard
	createCalls int
}

func (r *paymentFulfillmentUsageCardRepoStub) GetPlanByID(_ context.Context, id int64) (*UsageCardPlan, error) {
	if r.plan == nil || r.plan.ID != id {
		return nil, ErrUsageCardPlanNotFound
	}
	plan := *r.plan
	return &plan, nil
}

func (r *paymentFulfillmentUsageCardRepoStub) GetCardBySourceOrderID(_ context.Context, orderID int64) (*UserUsageCard, error) {
	if r.cardsByOrder != nil {
		if card := r.cardsByOrder[orderID]; card != nil {
			cp := *card
			return &cp, nil
		}
	}
	return nil, ErrUsageCardNotFound
}

func (r *paymentFulfillmentUsageCardRepoStub) CreateCard(_ context.Context, input CreateUsageCardInput) (*UserUsageCard, error) {
	r.createCalls++
	if r.cardsByOrder == nil {
		r.cardsByOrder = map[int64]*UserUsageCard{}
	}
	orderID := int64(0)
	if input.SourceOrderID != nil {
		orderID = *input.SourceOrderID
	}
	card := &UserUsageCard{
		ID:               int64(1000 + r.createCalls),
		UserID:           input.UserID,
		PlanID:           input.PlanID,
		Name:             input.Name,
		StartsAt:         input.StartsAt,
		ExpiresAt:        input.ExpiresAt,
		TotalLimitUSD:    input.TotalLimitUSD,
		Status:           UsageCardStatusActive,
		Source:           input.Source,
		SourceOrderID:    input.SourceOrderID,
		SourceRedeemCode: input.SourceRedeemCode,
		Notes:            input.Notes,
	}
	if orderID > 0 {
		r.cardsByOrder[orderID] = card
	}
	cp := *card
	return &cp, nil
}
```

- [x] **Step 2: Add failing invited-buyer fulfillment test**

Add this test in `backend/internal/service/payment_fulfillment_test.go` after the subscription affiliate fulfillment tests:

```go
func TestExecuteUsageCardFulfillmentAccruesAffiliateRebateFromPayAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)

	user, err := client.User.Create().
		SetEmail("usage-card-affiliate@example.com").
		SetPasswordHash("hash").
		SetUsername("usage-card-affiliate-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(500).
		SetPayAmount(59.9).
		SetFeeRate(0).
		SetRechargeCode("PAY-USAGE-CARD-AFFILIATE").
		SetOutTradeNo("sub2_usage_card_affiliate").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-usage-card-affiliate").
		SetOrderType(payment.OrderTypeUsageCard).
		SetPlanID(88).
		SetStatus(OrderStatusPaid).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	inviterID := int64(9001)
	affiliateRepo := &paymentFulfillmentAffiliateRepoStub{
		inviteeSummary: &AffiliateSummary{
			UserID:    user.ID,
			AffCode:   "INVITEE",
			InviterID: &inviterID,
			CreatedAt: time.Now().Add(-24 * time.Hour),
		},
		inviterSummary: &AffiliateSummary{
			UserID:    inviterID,
			AffCode:   "INVITER",
			CreatedAt: time.Now().Add(-48 * time.Hour),
		},
	}
	settingRepo := &paymentFulfillmentSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:           "true",
		SettingKeyAffiliateRebateRate:        "20",
		SettingKeyAffiliateRebateFreezeHours: "0",
		SettingKeyUsageCardEnabled:           "true",
		SettingKeyUsageCardPaymentEnabled:    "true",
	}}
	settingSvc := NewSettingService(settingRepo, nil)
	usageRepo := &paymentFulfillmentUsageCardRepoStub{
		plan: &UsageCardPlan{ID: 88, Name: "Usage 500", Price: 59.9, AmountUSD: 500, ValidityDays: 30, ForSale: true},
	}
	svc := &PaymentService{
		entClient:        client,
		usageCardService: NewUsageCardService(usageRepo, settingRepo),
		affiliateService: NewAffiliateService(affiliateRepo, settingSvc, nil, nil),
	}

	err = svc.ExecuteUsageCardFulfillment(ctx, order.ID)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, 1, usageRepo.createCalls)
	require.Len(t, affiliateRepo.accrueCalls, 1)
	require.Equal(t, inviterID, affiliateRepo.accrueCalls[0].inviterID)
	require.Equal(t, user.ID, affiliateRepo.accrueCalls[0].inviteeUserID)
	require.InDelta(t, 11.98, affiliateRepo.accrueCalls[0].amount, 0.000001)
	require.NotNil(t, affiliateRepo.accrueCalls[0].sourceOrderID)
	require.Equal(t, order.ID, *affiliateRepo.accrueCalls[0].sourceOrderID)

	applied, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("AFFILIATE_REBATE_APPLIED")).
		Only(ctx)
	require.NoError(t, err)
	require.Contains(t, applied.Detail, `"baseAmount":59.9`)
	require.Contains(t, applied.Detail, `"rebateAmount":11.98`)
}
```

- [x] **Step 3: Run test to verify it fails**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run TestExecuteUsageCardFulfillmentAccruesAffiliateRebateFromPayAmount -count=1
```

Expected before implementation: FAIL because usage card fulfillment completes without `AFFILIATE_REBATE_APPLIED`.

- [x] **Step 4: Invoke rebate processing from usage card fulfillment**

Change `doUsageCard` in `backend/internal/service/payment_fulfillment.go` to call affiliate rebate processing before completion:

```go
func (s *PaymentService) doUsageCard(ctx context.Context, o *dbent.PaymentOrder) error {
	if s.usageCardService == nil {
		return ErrUsageCardPaymentDisabled
	}
	if o.PlanID == nil || *o.PlanID <= 0 {
		return infraerrors.BadRequest("INVALID_USAGE_CARD_ORDER", "usage card order missing plan")
	}
	if s.hasAuditLog(ctx, o.ID, "USAGE_CARD_SUCCESS") {
		if err := s.applyAffiliateRebateForOrder(ctx, o); err != nil {
			return err
		}
		return s.markCompleted(ctx, o, "USAGE_CARD_SUCCESS")
	}
	plan, err := s.usageCardService.GetPlanByID(ctx, *o.PlanID)
	if err != nil {
		return fmt.Errorf("get usage card plan: %w", err)
	}
	if _, err := s.usageCardService.IssueFromPayment(ctx, o.UserID, plan, o.ID, o.RechargeCode); err != nil {
		return fmt.Errorf("issue usage card: %w", err)
	}
	if err := s.applyAffiliateRebateForOrder(ctx, o); err != nil {
		return err
	}
	return s.markCompleted(ctx, o, "USAGE_CARD_SUCCESS")
}
```

- [x] **Step 5: Run focused fulfillment test**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestAffiliateRebateBaseAmountByOrderType|TestExecuteUsageCardFulfillmentAccruesAffiliateRebateFromPayAmount' -count=1
```

Expected after implementation: PASS.

- [x] **Step 6: Mark OpenSpec usage card fulfillment task**

In `openspec/changes/usage-card-affiliate-rebate/tasks.md`, mark these tasks complete:

```md
- [x] 2.1 Invoke affiliate rebate processing during successful usage card payment fulfillment after idempotent card issuance.
- [x] 3.1 Add backend tests proving invited usage card buyers accrue inviter rebate from the usage card order `pay_amount`.
```

- [x] **Step 7: Commit Task 2**

Run:

```bash
git add backend/internal/service/payment_fulfillment.go backend/internal/service/payment_fulfillment_test.go openspec/changes/usage-card-affiliate-rebate/tasks.md
git commit -m "feat: apply affiliate rebate to usage card fulfillment"
```

## Task 3: Skipped Rebate, Status Gates, And Retry Idempotency

**Files:**
- Modify: `backend/internal/service/payment_fulfillment_test.go`
- Track: `openspec/changes/usage-card-affiliate-rebate/tasks.md`

- [x] **Step 1: Extend affiliate repo stub for cap testing**

Modify `paymentFulfillmentAffiliateRepoStub` in `backend/internal/service/payment_fulfillment_test.go` so it can simulate an existing per-invitee rebate amount:

```go
type paymentFulfillmentAffiliateRepoStub struct {
	inviteeSummary      *AffiliateSummary
	inviterSummary      *AffiliateSummary
	accruedFromInvitee  float64
	accrueCalls         []paymentFulfillmentAffiliateAccrueCall
}
```

Change `GetAccruedRebateFromInvitee` to:

```go
func (r *paymentFulfillmentAffiliateRepoStub) GetAccruedRebateFromInvitee(context.Context, int64, int64) (float64, error) {
	return r.accruedFromInvitee, nil
}
```

- [x] **Step 2: Add skipped-rebate table test**

Add this test in `backend/internal/service/payment_fulfillment_test.go`. It covers disabled affiliate settings, no inviter, zero rebate rate, expired duration, and reached per-invitee cap:

```go
func TestExecuteUsageCardFulfillmentSkipsIneligibleAffiliateRebate(t *testing.T) {
	cases := []struct {
		name               string
		withInviter        bool
		inviteeCreatedAt   time.Time
		settingOverrides   map[string]string
		accruedFromInvitee float64
	}{
		{
			name:             "affiliate disabled",
			withInviter:      true,
			inviteeCreatedAt: time.Now().Add(-24 * time.Hour),
			settingOverrides: map[string]string{SettingKeyAffiliateEnabled: "false"},
		},
		{
			name:             "no inviter",
			withInviter:      false,
			inviteeCreatedAt: time.Now().Add(-24 * time.Hour),
		},
		{
			name:             "zero rebate rate",
			withInviter:      true,
			inviteeCreatedAt: time.Now().Add(-24 * time.Hour),
			settingOverrides: map[string]string{SettingKeyAffiliateRebateRate: "0"},
		},
		{
			name:             "expired duration",
			withInviter:      true,
			inviteeCreatedAt: time.Now().Add(-48 * time.Hour),
			settingOverrides: map[string]string{SettingKeyAffiliateRebateDurationDays: "1"},
		},
		{
			name:               "cap reached",
			withInviter:        true,
			inviteeCreatedAt:   time.Now().Add(-24 * time.Hour),
			settingOverrides:   map[string]string{SettingKeyAffiliateRebatePerInviteeCap: "5"},
			accruedFromInvitee: 5,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
			suffix := strconv.Itoa(i)

			user, err := client.User.Create().
				SetEmail("usage-card-affiliate-skip-" + suffix + "@example.com").
				SetPasswordHash("hash").
				SetUsername("usage-card-affiliate-skip-" + suffix).
				Save(ctx)
			require.NoError(t, err)

			order, err := client.PaymentOrder.Create().
				SetUserID(user.ID).
				SetUserEmail(user.Email).
				SetUserName(user.Username).
				SetAmount(500).
				SetPayAmount(59.9).
				SetFeeRate(0).
				SetRechargeCode("PAY-USAGE-CARD-AFFILIATE-SKIP-" + suffix).
				SetOutTradeNo("sub2_usage_card_affiliate_skip_" + suffix).
				SetPaymentType(payment.TypeAlipay).
				SetPaymentTradeNo("trade-usage-card-affiliate-skip-" + suffix).
				SetOrderType(payment.OrderTypeUsageCard).
				SetPlanID(88).
				SetStatus(OrderStatusPaid).
				SetExpiresAt(time.Now().Add(time.Hour)).
				SetClientIP("127.0.0.1").
				SetSrcHost("api.example.com").
				Save(ctx)
			require.NoError(t, err)

			inviterID := int64(9001 + i)
			inviteeSummary := &AffiliateSummary{
				UserID:    user.ID,
				AffCode:   "INVITEE-" + suffix,
				CreatedAt: tc.inviteeCreatedAt,
			}
			if tc.withInviter {
				inviteeSummary.InviterID = &inviterID
			}

			values := map[string]string{
				SettingKeyAffiliateEnabled:             "true",
				SettingKeyAffiliateRebateRate:          "20",
				SettingKeyAffiliateRebateFreezeHours:   "0",
				SettingKeyAffiliateRebateDurationDays:  "0",
				SettingKeyAffiliateRebatePerInviteeCap: "0",
				SettingKeyUsageCardEnabled:             "true",
				SettingKeyUsageCardPaymentEnabled:      "true",
			}
			for key, value := range tc.settingOverrides {
				values[key] = value
			}
			settingRepo := &paymentFulfillmentSettingRepoStub{values: values}
			settingSvc := NewSettingService(settingRepo, nil)
			affiliateRepo := &paymentFulfillmentAffiliateRepoStub{
				inviteeSummary:      inviteeSummary,
				inviterSummary:      &AffiliateSummary{UserID: inviterID, AffCode: "INVITER-" + suffix, CreatedAt: time.Now().Add(-48 * time.Hour)},
				accruedFromInvitee:  tc.accruedFromInvitee,
			}
			usageRepo := &paymentFulfillmentUsageCardRepoStub{
				plan: &UsageCardPlan{ID: 88, Name: "Usage 500", Price: 59.9, AmountUSD: 500, ValidityDays: 30, ForSale: true},
			}
			svc := &PaymentService{
				entClient:        client,
				usageCardService: NewUsageCardService(usageRepo, settingRepo),
				affiliateService: NewAffiliateService(affiliateRepo, settingSvc, nil, nil),
			}

			err = svc.ExecuteUsageCardFulfillment(ctx, order.ID)
			require.NoError(t, err)

			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusCompleted, reloaded.Status)
			require.Equal(t, 1, usageRepo.createCalls)
			require.Empty(t, affiliateRepo.accrueCalls)

			skipped, err := client.PaymentAuditLog.Query().
				Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("AFFILIATE_REBATE_SKIPPED")).
				Only(ctx)
			require.NoError(t, err)
			require.Contains(t, skipped.Detail, `"baseAmount":59.9`)
		})
	}
}
```

- [x] **Step 3: Add non-success status regression test**

Add this test in `backend/internal/service/payment_fulfillment_test.go`. It proves pending and refund-related usage card orders do not issue cards or accrue rebate, while already completed orders no-op:

```go
func TestExecuteUsageCardFulfillmentNonSuccessfulStatusesDoNotAccrueAffiliateRebate(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		wantError bool
	}{
		{name: "pending", status: OrderStatusPending, wantError: true},
		{name: "cancelled", status: OrderStatusCancelled, wantError: true},
		{name: "expired", status: OrderStatusExpired, wantError: true},
		{name: "refund requested", status: OrderStatusRefundRequested, wantError: true},
		{name: "refunding", status: OrderStatusRefunding, wantError: true},
		{name: "completed", status: OrderStatusCompleted, wantError: false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
			suffix := strconv.Itoa(i)

			user, err := client.User.Create().
				SetEmail("usage-card-affiliate-status-" + suffix + "@example.com").
				SetPasswordHash("hash").
				SetUsername("usage-card-affiliate-status-" + suffix).
				Save(ctx)
			require.NoError(t, err)

			order, err := client.PaymentOrder.Create().
				SetUserID(user.ID).
				SetUserEmail(user.Email).
				SetUserName(user.Username).
				SetAmount(500).
				SetPayAmount(59.9).
				SetFeeRate(0).
				SetRechargeCode("PAY-USAGE-CARD-AFFILIATE-STATUS-" + suffix).
				SetOutTradeNo("sub2_usage_card_affiliate_status_" + suffix).
				SetPaymentType(payment.TypeAlipay).
				SetPaymentTradeNo("trade-usage-card-affiliate-status-" + suffix).
				SetOrderType(payment.OrderTypeUsageCard).
				SetPlanID(88).
				SetStatus(tc.status).
				SetExpiresAt(time.Now().Add(time.Hour)).
				SetClientIP("127.0.0.1").
				SetSrcHost("api.example.com").
				Save(ctx)
			require.NoError(t, err)

			inviterID := int64(9101 + i)
			affiliateRepo := &paymentFulfillmentAffiliateRepoStub{
				inviteeSummary: &AffiliateSummary{UserID: user.ID, AffCode: "INVITEE-" + suffix, InviterID: &inviterID, CreatedAt: time.Now().Add(-24 * time.Hour)},
				inviterSummary: &AffiliateSummary{UserID: inviterID, AffCode: "INVITER-" + suffix, CreatedAt: time.Now().Add(-48 * time.Hour)},
			}
			settingRepo := &paymentFulfillmentSettingRepoStub{values: map[string]string{
				SettingKeyAffiliateEnabled:        "true",
				SettingKeyAffiliateRebateRate:     "20",
				SettingKeyUsageCardEnabled:        "true",
				SettingKeyUsageCardPaymentEnabled: "true",
			}}
			settingSvc := NewSettingService(settingRepo, nil)
			usageRepo := &paymentFulfillmentUsageCardRepoStub{
				plan: &UsageCardPlan{ID: 88, Name: "Usage 500", Price: 59.9, AmountUSD: 500, ValidityDays: 30, ForSale: true},
			}
			svc := &PaymentService{
				entClient:        client,
				usageCardService: NewUsageCardService(usageRepo, settingRepo),
				affiliateService: NewAffiliateService(affiliateRepo, settingSvc, nil, nil),
			}

			err = svc.ExecuteUsageCardFulfillment(ctx, order.ID)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, usageRepo.createCalls)
			require.Empty(t, affiliateRepo.accrueCalls)

			appliedCount, err := client.PaymentAuditLog.Query().
				Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("AFFILIATE_REBATE_APPLIED")).
				Count(ctx)
			require.NoError(t, err)
			require.Zero(t, appliedCount)
		})
	}
}
```

- [x] **Step 4: Add idempotent retry test**

Add this test in `backend/internal/service/payment_fulfillment_test.go`:

```go
func TestExecuteUsageCardFulfillmentDoesNotDuplicateCardOrAffiliateRebate(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)

	user, err := client.User.Create().
		SetEmail("usage-card-affiliate-idempotent@example.com").
		SetPasswordHash("hash").
		SetUsername("usage-card-affiliate-idempotent-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(500).
		SetPayAmount(59.9).
		SetFeeRate(0).
		SetRechargeCode("PAY-USAGE-CARD-AFFILIATE-IDEMPOTENT").
		SetOutTradeNo("sub2_usage_card_affiliate_idempotent").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-usage-card-affiliate-idempotent").
		SetOrderType(payment.OrderTypeUsageCard).
		SetPlanID(88).
		SetStatus(OrderStatusPaid).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	inviterID := int64(9001)
	affiliateRepo := &paymentFulfillmentAffiliateRepoStub{
		inviteeSummary: &AffiliateSummary{
			UserID:    user.ID,
			AffCode:   "INVITEE",
			InviterID: &inviterID,
			CreatedAt: time.Now().Add(-24 * time.Hour),
		},
		inviterSummary: &AffiliateSummary{
			UserID:    inviterID,
			AffCode:   "INVITER",
			CreatedAt: time.Now().Add(-48 * time.Hour),
		},
	}
	settingRepo := &paymentFulfillmentSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:        "true",
		SettingKeyAffiliateRebateRate:     "20",
		SettingKeyUsageCardEnabled:        "true",
		SettingKeyUsageCardPaymentEnabled: "true",
	}}
	settingSvc := NewSettingService(settingRepo, nil)
	usageRepo := &paymentFulfillmentUsageCardRepoStub{
		plan: &UsageCardPlan{ID: 88, Name: "Usage 500", Price: 59.9, AmountUSD: 500, ValidityDays: 30, ForSale: true},
	}
	svc := &PaymentService{
		entClient:        client,
		usageCardService: NewUsageCardService(usageRepo, settingRepo),
		affiliateService: NewAffiliateService(affiliateRepo, settingSvc, nil, nil),
	}

	err = svc.ExecuteUsageCardFulfillment(ctx, order.ID)
	require.NoError(t, err)

	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusPaid).Save(ctx)
	require.NoError(t, err)

	err = svc.ExecuteUsageCardFulfillment(ctx, order.ID)
	require.NoError(t, err)

	require.Equal(t, 1, usageRepo.createCalls)
	require.Len(t, affiliateRepo.accrueCalls, 1)

	appliedCount, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("AFFILIATE_REBATE_APPLIED")).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, appliedCount)
}
```

- [x] **Step 5: Run tests to verify behavior**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestExecuteUsageCardFulfillment.*Affiliate|TestExecuteUsageCardFulfillmentDoesNotDuplicateCardOrAffiliateRebate|TestExecuteUsageCardFulfillmentNonSuccessfulStatusesDoNotAccrueAffiliateRebate|TestExecuteUsageCardFulfillmentFailedStatusRetriesAndAccruesAffiliateRebate' -count=1
```

Expected: PASS after Task 2 implementation. If any test fails, fix the test setup or implementation before continuing.

- [x] **Step 6: Mark OpenSpec retry, skip, and status tasks**

In `openspec/changes/usage-card-affiliate-rebate/tasks.md`, mark these tasks complete:

```md
- [x] 2.2 Preserve retry behavior so previously issued usage cards and previously applied or skipped affiliate rebates are not duplicated.
- [x] 2.3 Preserve existing behavior for paid, failed, completed, and refund-related usage card order statuses.
- [x] 3.2 Add backend tests proving disabled affiliate settings, no inviter, zero rebate, expired duration, or reached per-invitee cap skip rebate without blocking usage card fulfillment.
- [x] 3.3 Add backend tests proving repeated usage card fulfillment does not duplicate affiliate quota or audit results.
```

- [x] **Step 7: Commit Task 3**

Run:

```bash
git add backend/internal/service/payment_fulfillment_test.go openspec/changes/usage-card-affiliate-rebate/tasks.md
git commit -m "test: cover usage card affiliate skip and retry"
```

## Task 4: Verification

**Files:**
- Track: `openspec/changes/usage-card-affiliate-rebate/tasks.md`

- [ ] **Step 1: Run targeted payment fulfillment tests**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestAffiliateRebateBaseAmountByOrderType|TestExecuteUsageCardFulfillment.*Affiliate|TestExecuteUsageCardFulfillmentDoesNotDuplicateCardOrAffiliateRebate|TestExecuteUsageCardFulfillmentNonSuccessfulStatusesDoNotAccrueAffiliateRebate|TestExecuteUsageCardFulfillmentFailedStatusRetriesAndAccruesAffiliateRebate|TestExecuteSubscriptionFulfillmentAppliesAffiliateRebate|TestExecuteSubscriptionFulfillmentDoesNotDuplicateWorkAfterLegacySuccessAudit' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader service unit tests**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -count=1
```

Expected: PASS.

- [ ] **Step 3: Validate OpenSpec change**

Run:

```bash
openspec validate usage-card-affiliate-rebate --strict
```

Expected: `Change 'usage-card-affiliate-rebate' is valid`.

- [ ] **Step 4: Mark verification tasks**

In `openspec/changes/usage-card-affiliate-rebate/tasks.md`, mark:

```md
- [x] 4.1 Run targeted payment fulfillment and affiliate service tests.
- [x] 4.2 Run the relevant OpenSpec validation for `usage-card-affiliate-rebate`.
```

- [ ] **Step 5: Commit verification task state**

Run:

```bash
git add openspec/changes/usage-card-affiliate-rebate/tasks.md
git commit -m "chore: mark usage card affiliate verification complete"
```

## Self-Review

- Spec coverage: Tasks cover `pay_amount` base amount, usage card fulfillment rebate invocation, skipped affiliate cases, retry idempotency, non-success statuses through existing `ExecuteUsageCardFulfillment` status gates, and OpenSpec validation.
- Placeholder scan: No placeholder markers, empty implementation notes, or unnamed test commands remain.
- Type consistency: Plan uses existing `PaymentOrder.PayAmount`, `payment.OrderTypeUsageCard`, `PaymentService.doUsageCard`, `UsageCardService.IssueFromPayment`, `AFFILIATE_REBATE_APPLIED`, and `AFFILIATE_REBATE_SKIPPED`.
