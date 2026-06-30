---
change: global-usage-card-available-summary
design-doc: docs/superpowers/specs/2026-06-30-global-usage-card-available-summary-design.md
base-ref: e7906fd6339d35afe286b553ede52ea8a147cb37
---

# Global Usage Card Available Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在全局顶栏新增余额卡剩余可用总量展示，并让 API 密钥页刷新按钮同步刷新顶栏余额卡信息和长期余额信息。

**Architecture:** 后端新增用户侧余额卡 summary 接口，复用现有 `ListAvailableCards` 可用口径计算数量和剩余总额。前端新增余额卡 summary API 与共享 Pinia store，`UsageCardMini` 从 store 读取可用数量和 `$0.00` 总量，API 密钥页刷新时调用该 store 和 `authStore.refreshUser()`。

**Tech Stack:** Go + Gin backend, Vue 3 + Pinia + TypeScript frontend, Vitest, Go testing with sqlmock/testify.

---

## File Structure

- Modify: `backend/internal/service/usage_card.go` for summary domain type.
- Modify: `backend/internal/service/usage_card_service.go` for `GetMySummary`.
- Create: `backend/internal/service/usage_card_service_test.go` for service-level summary tests.
- Modify: `backend/internal/handler/usage_card_handler.go` for summary response and handler.
- Modify: `backend/internal/server/routes/user.go` to register `GET /usage-cards/summary`.
- Modify: `frontend/src/api/usageCards.ts` for summary type and API method.
- Create: `frontend/src/stores/usageCardSummary.ts` for shared topbar summary state.
- Modify: `frontend/src/stores/index.ts` to export the new store.
- Modify: `frontend/src/components/common/UsageCardMini.vue` to display summary count and `$0.00` total.
- Create: `frontend/src/components/common/__tests__/UsageCardMini.spec.ts` for topbar summary display.
- Modify: `frontend/src/views/user/KeysView.vue` to refresh usage-card summary and long-term balance.
- Modify: `frontend/src/views/user/__tests__/KeysView.spec.ts` to cover refresh linkage.

## Task 1: Backend Usage-Card Summary

**Files:**
- Modify: `backend/internal/service/usage_card.go`
- Modify: `backend/internal/service/usage_card_service.go`
- Create: `backend/internal/service/usage_card_service_test.go`
- Modify: `backend/internal/handler/usage_card_handler.go`
- Modify: `backend/internal/server/routes/user.go`

- [x] **Step 1: Write the failing service test**

Create `backend/internal/service/usage_card_service_test.go` with a fake repository and this test:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type usageCardSummaryRepoStub struct {
	UsageCardRepository
	cards []UserUsageCard
	err   error
	now   time.Time
}

func (s *usageCardSummaryRepoStub) ListAvailableCards(ctx context.Context, userID int64, now time.Time) ([]UserUsageCard, error) {
	s.now = now
	return s.cards, s.err
}

func TestUsageCardServiceGetMySummaryCountsAndSumsAvailableCards(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	repo := &usageCardSummaryRepoStub{cards: []UserUsageCard{
		{ID: 1, TotalLimitUSD: 10, UsedUSD: 2},
		{ID: 2, TotalLimitUSD: 5.5, UsedUSD: 1.25},
	}}
	svc := NewUsageCardService(repo, nil)

	summary, err := svc.GetMySummary(context.Background(), 42, now)

	require.NoError(t, err)
	require.Equal(t, 2, summary.AvailableCount)
	require.InDelta(t, 12.25, summary.AvailableRemainingUSD, 0.000001)
	require.Equal(t, now, repo.now)
}
```

- [x] **Step 2: Run the service test and verify it fails**

Run:

```bash
cd backend && go test ./internal/service -run TestUsageCardServiceGetMySummaryCountsAndSumsAvailableCards -count=1
```

Expected: FAIL because `UsageCardSummary` and `GetMySummary` are not defined.

- [x] **Step 3: Add the summary domain type**

In `backend/internal/service/usage_card.go`, add:

```go
type UsageCardSummary struct {
	AvailableCount        int
	AvailableRemainingUSD float64
}
```

- [x] **Step 4: Implement the service method**

In `backend/internal/service/usage_card_service.go`, add after `HasAvailableCard`:

```go
func (s *UsageCardService) GetMySummary(ctx context.Context, userID int64, now time.Time) (*UsageCardSummary, error) {
	if !s.IsEnabled(ctx) {
		return &UsageCardSummary{}, nil
	}
	if s == nil || s.repo == nil {
		return &UsageCardSummary{}, nil
	}
	cards, err := s.repo.ListAvailableCards(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	summary := &UsageCardSummary{AvailableCount: len(cards)}
	for i := range cards {
		remaining := cards[i].RemainingUSD()
		if remaining > 0 {
			summary.AvailableRemainingUSD += remaining
		}
	}
	return summary, nil
}
```

- [x] **Step 5: Run the service test and verify it passes**

Run:

```bash
cd backend && go test ./internal/service -run TestUsageCardServiceGetMySummaryCountsAndSumsAvailableCards -count=1
```

Expected: PASS.

- [x] **Step 6: Add handler response and endpoint**

In `backend/internal/handler/usage_card_handler.go`, add:

```go
type usageCardSummaryResponse struct {
	AvailableCount        int     `json:"available_count"`
	AvailableRemainingUSD float64 `json:"available_remaining_usd"`
}

func (h *UsageCardHandler) GetMySummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	summary, err := h.usageCardService.GetMySummary(c.Request.Context(), subject.UserID, time.Now())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usageCardSummaryResponse{
		AvailableCount:        summary.AvailableCount,
		AvailableRemainingUSD: summary.AvailableRemainingUSD,
	})
}
```

In `backend/internal/server/routes/user.go`, update usage-card routes:

```go
usageCards := authenticated.Group("/usage-cards")
{
	usageCards.GET("/summary", h.UsageCard.GetMySummary)
	usageCards.GET("", h.UsageCard.ListMyCards)
}
```

- [x] **Step 7: Add repository-level coverage for available-card filtering**

Extend `backend/internal/repository/usage_card_repo_test.go` with:

```go
func TestUsageCardRepositoryListAvailableCardsUsesBillingAvailabilityCriteria(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageCardRepository(db)
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "plan_id", "name", "starts_at", "expires_at", "total_limit_usd",
		"used_usd", "status", "source", "source_order_id", "source_redeem_code",
		"assigned_by", "notes", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		int64(1), int64(42), sql.NullInt64{}, "active card", now.Add(-time.Hour), now.Add(time.Hour), 10.0,
		2.0, service.UsageCardStatusActive, service.UsageCardSourcePayment, sql.NullInt64{}, sql.NullString{},
		sql.NullInt64{}, sql.NullString{}, now, now, sql.NullTime{},
	)

	mock.ExpectQuery("status = 'active'[\\s\\S]*starts_at <= \\$2[\\s\\S]*expires_at > \\$2[\\s\\S]*used_usd < total_limit_usd").
		WithArgs(int64(42), now).
		WillReturnRows(rows)

	cards, err := repo.ListAvailableCards(context.Background(), 42, now)

	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, int64(1), cards[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}
```

- [x] **Step 8: Run backend usage-card tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/repository -run 'UsageCard' -count=1
```

Expected: PASS.

- [x] **Step 9: Commit backend summary**

Run:

```bash
git add backend/internal/service/usage_card.go backend/internal/service/usage_card_service.go backend/internal/service/usage_card_service_test.go backend/internal/handler/usage_card_handler.go backend/internal/server/routes/user.go backend/internal/repository/usage_card_repo_test.go
git commit -m "feat: add usage card summary endpoint"
```

## Task 2: Frontend Shared Usage-Card Summary State

**Files:**
- Modify: `frontend/src/api/usageCards.ts`
- Create: `frontend/src/stores/usageCardSummary.ts`
- Create: `frontend/src/stores/__tests__/usageCardSummary.spec.ts`
- Modify: `frontend/src/stores/index.ts`

- [x] **Step 1: Write the failing store test**

Create `frontend/src/stores/__tests__/usageCardSummary.spec.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useUsageCardSummaryStore } from '@/stores/usageCardSummary'

const getSummary = vi.fn()

vi.mock('@/api/usageCards', () => ({
  usageCardsAPI: {
    getSummary,
  },
}))

describe('useUsageCardSummaryStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getSummary.mockReset()
  })

  it('refreshes available count and remaining USD from the API', async () => {
    getSummary.mockResolvedValue({
      data: {
        available_count: 2,
        available_remaining_usd: 7.5,
      },
    })
    const store = useUsageCardSummaryStore()

    const summary = await store.refresh()

    expect(getSummary).toHaveBeenCalledTimes(1)
    expect(summary).toEqual({
      available_count: 2,
      available_remaining_usd: 7.5,
    })
    expect(store.availableCount).toBe(2)
    expect(store.availableRemainingUSD).toBe(7.5)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })
})
```

- [x] **Step 2: Run the store test and verify it fails**

Run:

```bash
cd frontend && pnpm test:run src/stores/__tests__/usageCardSummary.spec.ts
```

Expected: FAIL because `@/stores/usageCardSummary` does not exist yet.

- [x] **Step 3: Add the API type and method**

In `frontend/src/api/usageCards.ts`, add:

```ts
export interface UsageCardSummary {
  available_count: number
  available_remaining_usd: number
}
```

Then extend `usageCardsAPI`:

```ts
export const usageCardsAPI = {
  listMine() {
    return apiClient.get<UserUsageCard[]>('/usage-cards')
  },
  getSummary() {
    return apiClient.get<UsageCardSummary>('/usage-cards/summary')
  },
}
```

- [x] **Step 4: Create the summary store**

Create `frontend/src/stores/usageCardSummary.ts`:

```ts
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { usageCardsAPI, type UsageCardSummary } from '@/api/usageCards'

const emptySummary = (): UsageCardSummary => ({
  available_count: 0,
  available_remaining_usd: 0,
})

export const useUsageCardSummaryStore = defineStore('usageCardSummary', () => {
  const summary = ref<UsageCardSummary>(emptySummary())
  const loading = ref(false)
  const error = ref<unknown>(null)

  const availableCount = computed(() => summary.value.available_count)
  const availableRemainingUSD = computed(() => summary.value.available_remaining_usd)

  async function refresh(): Promise<UsageCardSummary> {
    loading.value = true
    error.value = null
    try {
      const res = await usageCardsAPI.getSummary()
      summary.value = {
        available_count: Number(res.data.available_count) || 0,
        available_remaining_usd: Number(res.data.available_remaining_usd) || 0,
      }
      return summary.value
    } catch (err) {
      error.value = err
      throw err
    } finally {
      loading.value = false
    }
  }

  function reset(): void {
    summary.value = emptySummary()
    error.value = null
    loading.value = false
  }

  return {
    summary,
    loading,
    error,
    availableCount,
    availableRemainingUSD,
    refresh,
    reset,
  }
})
```

- [x] **Step 5: Export the store**

In `frontend/src/stores/index.ts`, add:

```ts
export { useUsageCardSummaryStore } from './usageCardSummary'
```

- [x] **Step 6: Run the store test and verify it passes**

Run:

```bash
cd frontend && pnpm test:run src/stores/__tests__/usageCardSummary.spec.ts
```

Expected: PASS.

- [x] **Step 7: Run typecheck for the new store**

Run:

```bash
cd frontend && pnpm typecheck
```

Expected: PASS or only pre-existing unrelated failures. If unrelated failures exist, record the exact output before continuing.

- [x] **Step 8: Commit frontend summary state**

Run:

```bash
git add frontend/src/api/usageCards.ts frontend/src/stores/usageCardSummary.ts frontend/src/stores/__tests__/usageCardSummary.spec.ts frontend/src/stores/index.ts
git commit -m "feat: add usage card summary state"
```

## Task 3: Global Topbar Usage-Card Display

**Files:**
- Modify: `frontend/src/components/common/UsageCardMini.vue`
- Create: `frontend/src/components/common/__tests__/UsageCardMini.spec.ts`

- [x] **Step 1: Write the failing component test**

Create `frontend/src/components/common/__tests__/UsageCardMini.spec.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UsageCardMini from '../UsageCardMini.vue'

const { getSummary, listMine } = vi.hoisted(() => ({
  getSummary: vi.fn(),
  listMine: vi.fn(),
}))

vi.mock('@/api/usageCards', () => ({
  usageCardsAPI: {
    getSummary,
    listMine,
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'usageCards.availableSummary') return `$${params?.amount} available`
        if (key === 'usageCards.availableCount') return `${params?.count} available cards`
        return key
      },
    }),
  }
})

vi.mock('@/components/icons/Icon.vue', () => ({
  default: {
    props: ['name'],
    template: '<span data-test="icon">{{ name }}</span>',
  },
}))

describe('UsageCardMini', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getSummary.mockReset()
    listMine.mockReset()
    getSummary.mockResolvedValue({
      data: {
        available_count: 2,
        available_remaining_usd: 7.5,
      },
    })
    listMine.mockResolvedValue({ data: [] })
  })

  it('shows available usage-card count and topbar remaining total', async () => {
    const wrapper = mount(UsageCardMini, {
      global: {
        stubs: {
          RouterLink: true,
          transition: false,
        },
      },
    })
    await flushPromises()

    expect(getSummary).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('2')
    expect(wrapper.text()).toContain('$7.50')
  })
})
```

- [x] **Step 2: Run the component test and verify it fails**

Run:

```bash
cd frontend && pnpm test:run src/components/common/__tests__/UsageCardMini.spec.ts
```

Expected: FAIL because `UsageCardMini` does not call `getSummary` or show `$7.50`.

- [x] **Step 3: Update `UsageCardMini` imports and store usage**

In `frontend/src/components/common/UsageCardMini.vue`, import the store:

```ts
import { useUsageCardSummaryStore } from '@/stores/usageCardSummary'
```

Add:

```ts
const usageCardSummaryStore = useUsageCardSummaryStore()
const availableCount = computed(() => usageCardSummaryStore.availableCount)
const availableRemainingUSD = computed(() => usageCardSummaryStore.availableRemainingUSD)
```

- [x] **Step 4: Update topbar template**

Change the badge from `cards.length` to:

```vue
{{ availableCount }}
```

Add a compact amount next to the badge:

```vue
<span class="text-sm font-semibold tabular-nums text-primary-700 dark:text-primary-200">
  ${{ availableRemainingUSD.toFixed(2) }}
</span>
```

Keep the existing long-term account balance in `AppHeader.vue` unchanged.

- [x] **Step 5: Refresh summary on mount**

In `onMounted`, call both summary and list refreshes without coupling their failure paths:

```ts
onMounted(async () => {
  loading.value = true
  try {
    await Promise.allSettled([
      usageCardSummaryStore.refresh(),
      usageCardsAPI.listMine().then((res) => {
        cards.value = res.data
      }),
    ])
  } finally {
    loading.value = false
  }
})
```

- [x] **Step 6: Update hover summary copy**

Where the tooltip currently uses `cards.length`, use:

```vue
{{ availableCount > 0 ? t('usageCards.availableCount', { count: availableCount }) : t('usageCards.empty') }}
```

Add i18n keys in `frontend/src/i18n/locales/zh.ts` and `frontend/src/i18n/locales/en.ts`:

```ts
availableCount: '可用 {count} 张',
availableSummary: '可用余额 {amount}',
```

```ts
availableCount: '{count} available',
availableSummary: 'Available {amount}',
```

Use existing nearby `usageCards` locale objects.

- [x] **Step 7: Run the component test and verify it passes**

Run:

```bash
cd frontend && pnpm test:run src/components/common/__tests__/UsageCardMini.spec.ts
```

Expected: PASS.

- [x] **Step 8: Commit topbar display**

Run:

```bash
git add frontend/src/components/common/UsageCardMini.vue frontend/src/components/common/__tests__/UsageCardMini.spec.ts frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts
git commit -m "feat: show available usage card summary in topbar"
```

## Task 4: API Key Page Refresh Linkage

**Files:**
- Modify: `frontend/src/views/user/KeysView.vue`
- Modify: `frontend/src/views/user/__tests__/KeysView.spec.ts`

- [ ] **Step 1: Write the failing KeysView test**

In `frontend/src/views/user/__tests__/KeysView.spec.ts`, add hoisted mocks:

```ts
const refreshUser = vi.fn()
const refreshUsageCardSummary = vi.fn()
```

Mock stores:

```ts
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser,
  }),
}))

vi.mock('@/stores/usageCardSummary', () => ({
  useUsageCardSummaryStore: () => ({
    refresh: refreshUsageCardSummary,
  }),
}))
```

In `beforeEach`, reset and resolve:

```ts
refreshUser.mockReset()
refreshUsageCardSummary.mockReset()
refreshUser.mockResolvedValue({})
refreshUsageCardSummary.mockResolvedValue({})
```

Add the test:

```ts
it('refreshes topbar usage-card summary and long-term balance when the refresh button is clicked', async () => {
  const wrapper = await mountView()

  listKeys.mockClear()
  await wrapper.get('button[title="Refresh"]').trigger('click')
  await flushPromises()

  expect(listKeys).toHaveBeenCalledTimes(1)
  expect(refreshUsageCardSummary).toHaveBeenCalledTimes(1)
  expect(refreshUser).toHaveBeenCalledTimes(1)
})
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
cd frontend && pnpm test:run src/views/user/__tests__/KeysView.spec.ts
```

Expected: FAIL because `KeysView` does not call the topbar refresh functions yet.

- [ ] **Step 3: Update KeysView imports and store setup**

In `frontend/src/views/user/KeysView.vue`, update imports:

```ts
import { useAuthStore } from '@/stores/auth'
import { useUsageCardSummaryStore } from '@/stores/usageCardSummary'
```

Add setup variables near other stores:

```ts
const authStore = useAuthStore()
const usageCardSummaryStore = useUsageCardSummaryStore()
```

- [ ] **Step 4: Add non-blocking topbar refresh helper**

Add:

```ts
const refreshTopbarFunds = async () => {
  const results = await Promise.allSettled([
    usageCardSummaryStore.refresh(),
    authStore.refreshUser(),
  ])
  results.forEach((result, index) => {
    if (result.status === 'rejected') {
      console.error(index === 0 ? 'Failed to refresh usage card summary:' : 'Failed to refresh user balance:', result.reason)
    }
  })
}
```

- [ ] **Step 5: Call topbar refresh from `loadApiKeys`**

At the start of `loadApiKeys`, after creating `signal`, add:

```ts
const topbarRefresh = refreshTopbarFunds()
```

Before leaving the `try` block or in `finally`, wait without throwing:

```ts
topbarRefresh.catch((error) => {
  console.error('Failed to refresh topbar funds:', error)
})
```

Do not let `refreshTopbarFunds` abort or block API key list updates.

- [ ] **Step 6: Run the KeysView test and verify it passes**

Run:

```bash
cd frontend && pnpm test:run src/views/user/__tests__/KeysView.spec.ts
```

Expected: PASS.

- [ ] **Step 7: Commit refresh linkage**

Run:

```bash
git add frontend/src/views/user/KeysView.vue frontend/src/views/user/__tests__/KeysView.spec.ts
git commit -m "feat: refresh topbar funds from api keys page"
```

## Task 5: Verification And OpenSpec Task Sync

**Files:**
- Modify: `openspec/changes/global-usage-card-available-summary/tasks.md`

- [ ] **Step 1: Run targeted backend tests**

Run:

```bash
cd backend && go test ./internal/service ./internal/repository -run 'UsageCard' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run targeted frontend tests**

Run:

```bash
cd frontend && pnpm test:run src/components/common/__tests__/UsageCardMini.spec.ts src/views/user/__tests__/KeysView.spec.ts
```

Expected: PASS.

- [ ] **Step 3: Run frontend typecheck**

Run:

```bash
cd frontend && pnpm typecheck
```

Expected: PASS or documented pre-existing unrelated failures.

- [ ] **Step 4: Validate OpenSpec**

Run:

```bash
openspec validate global-usage-card-available-summary
```

Expected: PASS.

- [ ] **Step 5: Sync OpenSpec task checklist**

Update `openspec/changes/global-usage-card-available-summary/tasks.md` so completed items are checked:

```md
- [x] 1.1 Add a user usage-card summary response containing available card count and available remaining USD.
- [x] 1.2 Reuse the existing server-side available-card criteria for active, started, unexpired, undeleted, non-exhausted cards.
- [x] 1.3 Register the authenticated user route and add backend tests for active, expired, exhausted, suspended, cancelled, and future cards.
- [x] 2.1 Add a frontend API method and type for loading the usage-card summary.
- [x] 2.2 Add a shared store/composable refresh entry point for topbar usage-card summary data.
- [x] 2.3 Add or reuse a refresh entry point for the existing topbar long-term account balance.
- [x] 2.4 Ensure usage-card summary and long-term balance refresh failures do not block callers that also refresh unrelated page data.
- [x] 3.1 Change `UsageCardMini` to show available card count instead of total card count.
- [x] 3.2 Show the available remaining USD total directly in the global topbar component.
- [x] 3.3 Keep the existing long-term balance display unchanged and visually separate from the usage-card remaining total.
- [x] 3.4 Align the hover/summary copy with the available-card summary while preserving useful card details.
- [x] 3.5 Add or update frontend tests for zero cards, mixed unavailable cards, separated long-term balance display, and formatted summary display.
- [x] 4.1 Update the API key page refresh action to call the shared usage-card summary refresh and long-term balance refresh.
- [x] 4.2 Add or update tests proving the API key refresh triggers both topbar usage-card information refresh and long-term balance refresh.
- [x] 5.1 Run targeted backend usage-card tests.
- [x] 5.2 Run targeted frontend tests or type checks for the topbar and API key page.
- [x] 5.3 Validate the OpenSpec change artifacts.
```

- [ ] **Step 6: Commit verification and checklist**

Run:

```bash
git add openspec/changes/global-usage-card-available-summary/tasks.md
git commit -m "chore: complete usage card summary checklist"
```

## Self-Review

- Spec coverage: The plan covers backend summary API, topbar separate display, API key refresh linkage, non-blocking side refresh failures, and verification.
- Placeholder scan: No placeholder implementation steps remain.
- Type consistency: Backend uses `UsageCardSummary.AvailableCount` and `AvailableRemainingUSD`; frontend API uses `available_count` and `available_remaining_usd`; store exposes `availableCount` and `availableRemainingUSD`.
