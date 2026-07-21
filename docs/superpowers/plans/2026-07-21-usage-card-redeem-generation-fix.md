# Usage Card Redeem Generation Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure newly generated usage-card redeem codes retain a valid usage-card plan and use the plan's balance value.

**Architecture:** Keep the public admin API and repository contracts unchanged. Add generation-side validation in `adminServiceImpl.GenerateRedeemCodes`, load the selected plan through the service's existing Ent client before creating any codes, and copy the plan ID plus plan-derived amount into every persisted `RedeemCode`.

**Tech Stack:** Go, Ent, modernc SQLite, testify, Docker, nginx

---

### Task 1: Add Generation-Side Regression Coverage

**Files:**
- Create: `backend/internal/service/admin_service_generate_redeem_codes_test.go`

- [ ] **Step 1: Add a capturing repository and SQLite-backed Ent test client**

```go
//go:build unit

package service

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type generatedRedeemCodeRepoStub struct {
	RedeemCodeRepository
	created []RedeemCode
}

func (r *generatedRedeemCodeRepoStub) Create(_ context.Context, code *RedeemCode) error {
	clone := *code
	r.created = append(r.created, clone)
	return nil
}

func newAdminServiceGenerateRedeemCodesTestClient(t *testing.T, databaseName string) *dbent.Client {
	t.Helper()

	database, err := sql.Open("sqlite", "file:"+databaseName+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, database)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
```

- [ ] **Step 2: Write the failing valid-plan test**

Create an `UsageCardPlan` with `AmountUsd: 250`, call `GenerateRedeemCodes` with `Type: RedeemTypeUsageCard`, a deliberately incorrect request `Value`, and the plan ID, then assert:

```go
func TestAdminServiceGenerateUsageCardRedeemCodesUsesPlan(t *testing.T) {
	client := newAdminServiceGenerateRedeemCodesTestClient(t, "admin_generate_redeem_valid")
	ctx := context.Background()
	plan, err := client.UsageCardPlan.Create().
		SetName("Usage 250").
		SetPrice(25).
		SetAmountUsd(250).
		SetValidityDays(30).
		Save(ctx)
	require.NoError(t, err)

	repo := &generatedRedeemCodeRepoStub{}
	service := &adminServiceImpl{redeemCodeRepo: repo, entClient: client}
	planID := int64(plan.ID)
	codes, err := service.GenerateRedeemCodes(ctx, &GenerateRedeemCodesInput{
		Count:           1,
		Type:            RedeemTypeUsageCard,
		Value:           999,
		UsageCardPlanID: &planID,
	})

require.NoError(t, err)
require.Len(t, codes, 1)
require.NotNil(t, codes[0].UsageCardPlanID)
require.Equal(t, plan.ID, *codes[0].UsageCardPlanID)
require.Equal(t, plan.AmountUsd, codes[0].Value)
require.NotNil(t, codes[0].UsageCardPlan)
require.Equal(t, plan.ID, codes[0].UsageCardPlan.ID)
require.Len(t, repo.created, 1)
require.Equal(t, codes[0], repo.created[0])
}
```

- [ ] **Step 3: Write failing malformed-request tests**

Table-test `usage_card` requests with a missing/zero plan ID, a non-empty `GroupID`, a non-zero `ValidityDays`, and a nonexistent plan. Each request must return an error and leave `repo.created` empty:

```go
func TestAdminServiceGenerateUsageCardRedeemCodesRejectsInvalidInput(t *testing.T) {
	client := newAdminServiceGenerateRedeemCodesTestClient(t, "admin_generate_redeem_invalid")
	zeroPlanID := int64(0)
	unknownPlanID := int64(999999)
	groupID := int64(7)
	tests := []struct {
		name  string
		input GenerateRedeemCodesInput
	}{
		{name: "missing plan", input: GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard}},
		{name: "zero plan", input: GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, UsageCardPlanID: &zeroPlanID}},
		{name: "group set", input: GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, GroupID: &groupID, UsageCardPlanID: &unknownPlanID}},
		{name: "validity set", input: GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, UsageCardPlanID: &unknownPlanID, ValidityDays: 30}},
		{name: "plan not found", input: GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, UsageCardPlanID: &unknownPlanID}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &generatedRedeemCodeRepoStub{}
			service := &adminServiceImpl{redeemCodeRepo: repo, entClient: client}
			_, err := service.GenerateRedeemCodes(context.Background(), &test.input)
			require.Error(t, err)
			require.Empty(t, repo.created)
		})
	}
}
```

- [ ] **Step 4: Run the focused test and verify RED**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestAdminServiceGenerateUsageCardRedeemCodes' -count=1
```

Expected: FAIL because the current generator neither validates nor persists `UsageCardPlanID` and keeps the request value.

### Task 2: Restore Usage-Card Generation Semantics

**Files:**
- Modify: `backend/internal/service/admin_user.go:1230`

- [ ] **Step 1: Add usage-card request validation before code generation**

Add the `usagecardplan` Ent predicate import, then validate and resolve the selected plan before the generation loop:

```go
var usageCardPlan *UsageCardPlan
redeemValue := input.Value

if input.Type == RedeemTypeUsageCard {
	if input.GroupID != nil {
		return nil, errors.New("group_id must be empty for usage card type")
	}
	if input.UsageCardPlanID == nil || *input.UsageCardPlanID <= 0 {
		return nil, errors.New("usage_card_plan_id is required for usage card type")
	}
	if input.ValidityDays != 0 {
		return nil, errors.New("validity_days must be empty for usage card type")
	}
	plan, err := s.getUsageCardPlanForRedeem(ctx, *input.UsageCardPlanID)
	if err != nil {
		return nil, err
	}
	usageCardPlan = plan
	redeemValue = plan.AmountUSD
}
```

- [ ] **Step 2: Resolve the plan before creating any code**

Restore the plan loader and translate the Ent model into the service model:

```go
func (s *adminServiceImpl) getUsageCardPlanForRedeem(ctx context.Context, id int64) (*UsageCardPlan, error) {
	if s == nil || s.entClient == nil {
		return nil, ErrUsageCardPlanNotFound
	}
	plan, err := s.entClient.UsageCardPlan.Query().
		Where(usagecardplan.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, ErrUsageCardPlanNotFound
		}
		return nil, err
	}
	return &UsageCardPlan{
		ID:           plan.ID,
		Name:         plan.Name,
		Description:  plan.Description,
		ProductName:  plan.ProductName,
		Price:        plan.Price,
		AmountUSD:    plan.AmountUsd,
		ValidityDays: plan.ValidityDays,
		Features:     plan.Features,
		ForSale:      plan.ForSale,
		SortOrder:    plan.SortOrder,
		CreatedAt:    plan.CreatedAt,
		UpdatedAt:    plan.UpdatedAt,
	}, nil
}
```

- [ ] **Step 3: Populate every generated record from the plan**

Use `redeemValue` for the code value, and set the usage-card association before persistence:

```go
code.Value = redeemValue

if input.Type == RedeemTypeUsageCard {
code.UsageCardPlanID = input.UsageCardPlanID
code.UsageCardPlan = usageCardPlan
}
```

before calling `redeemCodeRepo.Create`.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestAdminServiceGenerateUsageCardRedeemCodes' -count=1
```

Expected: PASS.

- [ ] **Step 5: Format and inspect the patch**

Run:

```bash
gofmt -w backend/internal/service/admin_user.go backend/internal/service/admin_service_generate_redeem_codes_test.go
git diff --check
git diff -- backend/internal/service/admin_user.go backend/internal/service/admin_service_generate_redeem_codes_test.go
```

### Task 3: Verify and Commit the Functional Fix

**Files:**
- Modify: `backend/internal/service/admin_user.go`
- Create: `backend/internal/service/admin_service_generate_redeem_codes_test.go`

- [ ] **Step 1: Run service regression coverage**

```bash
cd backend && go test -tags=unit ./internal/service -count=1
```

Expected: PASS.

- [ ] **Step 2: Run MDC1 local release validation**

```bash
git status --short
git branch --show-current
git log --oneline -3
git diff --check
cd backend && go test -tags=unit ./internal/handler/dto ./internal/service ./internal/handler ./internal/handler/admin -run 'APIKeyFromService|PaymentConfig|Checkout|OpenAIAccountScheduler|SettingHandler|GenerateUsageCardRedeemCodes' -count=1
cd frontend && npm run typecheck
```

Expected: all commands exit zero.

- [ ] **Step 3: Commit only the functional fix and test**

```bash
git add backend/internal/service/admin_user.go backend/internal/service/admin_service_generate_redeem_codes_test.go
git commit -m "fix: preserve usage card plan on generated redeem codes"
```

- [ ] **Step 4: Confirm the deployment source is clean**

```bash
git status --short
git log --oneline -3
```

Expected: empty status; functional fix commit is `HEAD`.

### Task 4: Deploy the Committed Snapshot to MDC1

**Files:**
- No repository changes

- [ ] **Step 1: Detect the active slot from nginx**

```bash
ssh mdc1 'docker exec nginx-ui nginx -T 2>/dev/null | grep -n "172.18.0.1:1808"'
```

- [ ] **Step 2: Archive committed HEAD, sync it, and build a unique image**

Use `git archive HEAD` into `mktemp -d`, `rsync --delete` that clean snapshot to `/home/pyj/sub2api-build/`, and build `sub2api:release-YYYYMMDDHHMMSS` on MDC1.

- [ ] **Step 3: Recreate only the standby app slot**

Copy environment variables from the healthy active app, remove any existing `HOMEPAGE_VARIANT`, append `HOMEPAGE_VARIANT=aixw`, and start the standby container on `sub2api_sub2api-network` with `/home/pyj/sub2api/data:/app/data`.

- [ ] **Step 4: Verify standby before traffic switch**

Require a healthy container, `GET /health` returning `{"status":"ok"}`, `GET /settings/public` returning `"homepage_variant":"aixw"`, the expected environment variable, and clean startup logs.

- [ ] **Step 5: Switch nginx and verify the live route**

Back up nginx configuration, replace the old fixed-slot port with the standby port, run `nginx -t`, reload, then verify the nginx upstream, live health response, homepage variant, container health, and restart count. Keep the former active slot for rollback and do not alter any redeem-code data.
