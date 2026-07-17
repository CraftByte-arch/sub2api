# Account Performance Account List and Detail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make account performance rows identifiable and operational by adding account metadata, successful/failed call counts, the shared platform badge, and a detail dialog that cannot leave the admin UI inert.

**Architecture:** Keep performance aggregation unchanged, then enrich the paged aggregate rows with account display metadata through outer joins. The frontend consumes additive response fields, derives failed calls from the existing canonical counters, and replaces the custom drawer shell with the established `BaseDialog` lifecycle while preserving the investigation content and API flow.

**Tech Stack:** Go 1.24, PostgreSQL, sqlmock/testify, Vue 3, TypeScript, Tailwind CSS, Vue Test Utils, Vitest.

---

## File Map

- Modify `backend/internal/service/account_performance.go`: add account display fields and two allowlisted sort constants.
- Modify `backend/internal/repository/account_performance_repo.go`: calculate `failure_count`, enrich aggregate rows from `accounts`, scan metadata, and allow success/failure sorting.
- Modify `backend/internal/repository/account_performance_repo_test.go`: prove query shape, metadata scanning, and sort allowlisting.
- Modify `backend/internal/repository/account_performance_repo_integration_test.go`: prove ordinary, shadow-parent, soft-deleted, and missing-account metadata behavior against PostgreSQL.
- Modify `frontend/src/api/admin/performance.ts`: type the additive account metadata fields.
- Modify `frontend/src/views/admin/performance/components/PerformanceAccountTable.vue`: render name/ID, platform badge, successful calls, and failed calls.
- Modify `frontend/src/views/admin/performance/components/__tests__/PerformanceAccountTable.spec.ts`: cover labels, badge props, counter formula, and sort events.
- Modify `frontend/src/views/admin/performance/components/PerformanceInvestigationDrawer.vue`: retain investigation content but delegate modal state to `BaseDialog`.
- Modify `frontend/src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts`: cover the shared dialog lifecycle and cleanup.
- Modify `frontend/src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts`: use enriched account fixtures and verify real selection/close cleanup integration.
- Modify `frontend/src/utils/__tests__/modalBodyLock.spec.ts`: add the new account metadata to its drawer fixture and retain the shared-lock assertions unchanged.

### Task 1: Enrich Account Performance Rows and Add Result Sorting

**Files:**
- Modify: `backend/internal/service/account_performance.go`
- Modify: `backend/internal/repository/account_performance_repo.go`
- Test: `backend/internal/repository/account_performance_repo_test.go`
- Test: `backend/internal/repository/account_performance_repo_integration_test.go`

- [ ] **Step 1: Write failing sort and metadata scan tests**

Extend the page-normalization table in `TestAccountPerformanceQueryRangeAndPageValidation`:

```go
service.AccountPerformanceSortSuccessCount: "success_count",
service.AccountPerformanceSortFailureCount: "failure_count",
```

Update `TestAccountPerformanceQueryAccountsUsesBoundedArgsAndAllowlistedSort` so the expected query requires account enrichment and its row includes the additive columns before counters. Build the columns and values from the repository's canonical metric list so their order cannot drift:

```go
columns := append([]string{
    "account_id", "platform", "account_name", "account_type", "auth_mode",
}, accountPerformanceMetricColumns...)
columns = append(columns, "availability", "failure_rate", "health_score", "total")

values := append([]driver.Value{
    int64(42), "openai", "Codex Team", "oauth", "personalAccessToken",
}, accountPerformanceMetricsValues()...)
values = append(values, float64(1), float64(0), float64(1), int64(1))

mock.ExpectQuery("(?s)LEFT JOIN accounts AS account ON account.id = scored.account_id.*LEFT JOIN accounts AS parent ON parent.id = account.parent_account_id").
    WithArgs(args...).
    WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
```

Add assertions:

```go
require.Equal(t, "Codex Team", page.Rows[0].AccountName)
require.Equal(t, "oauth", page.Rows[0].AccountType)
require.Equal(t, "personalAccessToken", page.Rows[0].AuthMode)
```

- [ ] **Step 2: Run the backend unit tests and verify RED**

Run:

```bash
go test ./internal/repository -run 'TestAccountPerformance(QueryRangeAndPageValidation|QueryAccountsUsesBoundedArgsAndAllowlistedSort)' -count=1
```

Expected: FAIL because the new sort constants and metadata fields do not exist and the query does not join `accounts`.

- [ ] **Step 3: Add service fields and stable sort constants**

In `backend/internal/service/account_performance.go`, add:

```go
const (
    AccountPerformanceSortHealthScore   = "health_score"
    AccountPerformanceSortAvailability  = "availability"
    AccountPerformanceSortFailureRate   = "failure_rate"
    AccountPerformanceSortP95TTFTMS     = "p95_ttft_ms"
    AccountPerformanceSortP95DurationMS = "p95_duration_ms"
    AccountPerformanceSortSamples       = "samples"
    AccountPerformanceSortSuccessCount  = "success_count"
    AccountPerformanceSortFailureCount  = "failure_count"
)

type AccountPerformanceAccount struct {
    AccountID    int64                      `json:"account_id"`
    AccountName  string                     `json:"account_name"`
    AccountType  string                     `json:"account_type"`
    AuthMode     string                     `json:"auth_mode,omitempty"`
    Platform     string                     `json:"platform"`
    Counters     AccountPerformanceCounters `json:"counters"`
    Availability float64                    `json:"availability"`
    FailureRate  float64                    `json:"failure_rate"`
    HealthScore  float64                    `json:"health_score"`
}
```

- [ ] **Step 4: Implement aggregate enrichment without changing metric semantics**

In `normalizeAccountPerformancePage`, extend the allowlist:

```go
service.AccountPerformanceSortSuccessCount: "success_count",
service.AccountPerformanceSortFailureCount: "failure_count",
```

In `QueryAccounts`, add `failure_count` to `scored`, then introduce an `enriched` CTE:

```sql
GREATEST(attempt_count - client_canceled_count - success_count, 0) AS failure_count,
attempt_count AS samples
FROM aggregated
), enriched AS (
SELECT
    scored.*,
    COALESCE(account.name, '#' || scored.account_id::text) AS account_name,
    COALESCE(account.type, '') AS account_type,
    COALESCE(
        NULLIF(account.credentials->>'auth_mode', ''),
        NULLIF(account.credentials->>'openai_auth_mode', ''),
        NULLIF(parent.credentials->>'auth_mode', ''),
        NULLIF(parent.credentials->>'openai_auth_mode', ''),
        ''
    ) AS auth_mode
FROM scored
LEFT JOIN accounts AS account ON account.id = scored.account_id
LEFT JOIN accounts AS parent ON parent.id = account.parent_account_id
)
SELECT account_id, platform, account_name, account_type, auth_mode, ` + accountPerformanceCounterColumns() + `,
       availability, failure_rate, health_score, COUNT(*) OVER() AS total
FROM enriched
ORDER BY ` + sortColumn + ` ` + sortOrder + `, account_id ASC
LIMIT $18 OFFSET $19
```

Do not filter either account join by `deleted_at`; soft-deleted rows are historical display metadata. Keep `COUNT(*) OVER()` after enrichment, and retain `ORDER BY <allowlisted column> <allowlisted order>, account_id ASC`.

Scan the new columns before the existing counters:

```go
destinations := append(
    []any{&item.AccountID, &item.Platform, &item.AccountName, &item.AccountType, &item.AuthMode},
    accountPerformanceCounterDestinations(&item.Counters)...,
)
```

- [ ] **Step 5: Run unit tests and verify GREEN**

Run:

```bash
go test ./internal/repository ./internal/service -run 'AccountPerformance' -count=1
```

Expected: PASS.

- [ ] **Step 6: Add PostgreSQL coverage for metadata fallbacks**

Add `TestAccountPerformanceQueryAccountsEnrichesDisplayMetadata` under the integration build tag. Insert a parent account, Spark shadow, soft-deleted account, and performance rows with a unique model. Assert:

```go
require.Equal(t, "Spark Shadow", byID[shadowID].AccountName)
require.Equal(t, "oauth", byID[shadowID].AccountType)
require.Equal(t, "personalAccessToken", byID[shadowID].AuthMode)
require.Equal(t, "Historical Account", byID[deletedID].AccountName)
require.Equal(t, fmt.Sprintf("#%d", missingID), byID[missingID].AccountName)
require.Empty(t, byID[missingID].AccountType)
```

Use `t.Cleanup` to delete only rows created by the test. Do not depend on fixed account IDs.

- [ ] **Step 7: Run the repository integration test when PostgreSQL is available**

Run the repository's established integration command with:

```bash
go test -tags=integration ./internal/repository -run 'TestAccountPerformanceQueryAccountsEnrichesDisplayMetadata' -count=1
```

Expected: PASS. If the local integration database is unavailable, record that exact environmental limitation and retain the compiling build-tagged test for CI.

- [ ] **Step 8: Commit backend enrichment**

```bash
git add backend/internal/service/account_performance.go backend/internal/repository/account_performance_repo.go backend/internal/repository/account_performance_repo_test.go backend/internal/repository/account_performance_repo_integration_test.go
git commit -m "feat: enrich account performance rows"
```

### Task 2: Redesign the Account Performance Table

**Files:**
- Modify: `frontend/src/api/admin/performance.ts`
- Modify: `frontend/src/views/admin/performance/components/PerformanceAccountTable.vue`
- Test: `frontend/src/views/admin/performance/components/__tests__/PerformanceAccountTable.spec.ts`
- Test: `frontend/src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts`

- [ ] **Step 1: Enrich the frontend fixture and write failing table assertions**

Add these fields to performance account fixtures:

```ts
account_name: 'Codex Team',
account_type: 'oauth',
auth_mode: 'personalAccessToken',
```

Mount with `PlatformTypeBadge` available and assert:

```ts
expect(wrapper.text()).toContain('Codex Team')
expect(wrapper.text()).toContain('#42')
const badge = wrapper.getComponent(PlatformTypeBadge)
expect(badge.props()).toMatchObject({
  platform: 'openai',
  type: 'oauth',
  authMode: 'personalAccessToken'
})
expect(wrapper.get('[data-testid="performance-success-count-42"]').text()).toBe('114')
expect(wrapper.get('[data-testid="performance-failure-count-42"]').text()).toBe('6')
```

Add a cancellation case with `attempt_count: 120`, `success_count: 90`, and `client_canceled_count: 20`; failed calls must render `10`, not `30`. Add an invalid-counter case where success exceeds eligible attempts; failed calls must render `0`.

Change the sorting assertion to click both result columns and expect:

```ts
expect(wrapper.emitted('sort')).toEqual([
  ['success_count'],
  ['failure_count']
])
```

- [ ] **Step 2: Run the component test and verify RED**

Run:

```bash
pnpm vitest run src/views/admin/performance/components/__tests__/PerformanceAccountTable.spec.ts --reporter=verbose
```

Expected: FAIL because the name, badge, result cells, and new sort events are absent.

- [ ] **Step 3: Add additive API types**

In `frontend/src/api/admin/performance.ts`, import the account unions and update the item type:

```ts
import type { AccountPlatform, AccountType } from '@/types'

export interface PerformanceAccountItem {
  account_id: number
  account_name: string
  account_type?: AccountType
  auth_mode?: string
  platform: AccountPlatform
}
```

Insert these fields into the existing interface without removing `counters`, `availability`, `failure_rate`, `health_score`, `p95_ttft_ms`, `p95_duration_ms`, or `low_sample`.

- [ ] **Step 4: Render account identity, shared badge, and canonical result counts**

In `PerformanceAccountTable.vue`, import `PlatformTypeBadge` and `PlatformIcon`. Add helpers:

```ts
function failedCalls(account: PerformanceAccount) {
  const eligible = Math.max(0, account.counters.attempt_count - account.counters.client_canceled_count)
  return Math.max(0, eligible - account.counters.success_count)
}

function platformLabel(platform: PerformanceAccount['platform']) {
  return ({ anthropic: 'Anthropic', openai: 'OpenAI', gemini: 'Gemini', antigravity: 'Antigravity', grok: 'Grok' })[platform]
}
```

Replace the first two cells with:

```vue
<td class="px-3 py-3">
  <button
    :data-testid="`performance-account-${item.account_id}`"
    type="button"
    class="rounded text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
    :aria-label="`查看账号 ${item.account_name || `#${item.account_id}`} 性能详情`"
    @click.stop="select(item)"
  >
    <span class="block font-medium text-gray-900 dark:text-white">{{ item.account_name || `#${item.account_id}` }}</span>
    <span class="mt-0.5 block font-mono text-xs text-gray-500 dark:text-gray-400">#{{ item.account_id }}</span>
  </button>
</td>
<td class="px-3 py-3">
  <PlatformTypeBadge
    v-if="item.account_type"
    :platform="item.platform"
    :type="item.account_type"
    :auth-mode="item.auth_mode"
  />
  <span v-else class="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-gray-700 dark:text-gray-200">
    <PlatformIcon :platform="item.platform" size="xs" />
    {{ platformLabel(item.platform) }}
  </span>
</td>
```

Replace “尝试次数” with two sortable headers and cells:

```vue
<button @click="emit('sort', 'success_count')">成功调用 <Icon name="sort" size="xs" /></button>
<button @click="emit('sort', 'failure_count')">失败调用 <Icon name="sort" size="xs" /></button>

<td :data-testid="`performance-success-count-${item.account_id}`" class="px-3 py-3 font-medium tabular-nums text-emerald-700 dark:text-emerald-300">
  {{ item.counters.success_count.toLocaleString() }}
</td>
<td :data-testid="`performance-failure-count-${item.account_id}`" class="px-3 py-3 font-medium tabular-nums text-red-700 dark:text-red-300">
  {{ failedCalls(item).toLocaleString() }}
</td>
```

- [ ] **Step 5: Update the parent-view fixtures and sorting test**

Add the metadata fields to `accounts.items[0]` in `AccountPerformanceView.spec.ts`. Replace the `sort-samples` stub control with `sort-success` and assert `getAccounts` receives `sort: 'success_count'` with the toggled order.

- [ ] **Step 6: Run focused frontend tests and verify GREEN**

Run:

```bash
pnpm vitest run src/views/admin/performance/components/__tests__/PerformanceAccountTable.spec.ts src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts --reporter=verbose
```

Expected: PASS.

- [ ] **Step 7: Commit the account table**

```bash
git add frontend/src/api/admin/performance.ts frontend/src/views/admin/performance/components/PerformanceAccountTable.vue frontend/src/views/admin/performance/components/__tests__/PerformanceAccountTable.spec.ts frontend/src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts
git commit -m "feat: clarify account performance results"
```

### Task 3: Replace the Custom Drawer Lifecycle with BaseDialog

**Files:**
- Modify: `frontend/src/views/admin/performance/components/PerformanceInvestigationDrawer.vue`
- Modify: `frontend/src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts`
- Modify: `frontend/src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts`
- Modify: `frontend/src/utils/__tests__/modalBodyLock.spec.ts`

- [ ] **Step 1: Write a failing lifecycle regression test**

Mount a real `PerformanceInvestigationDrawer` inside an `#app` element, with chart components stubbed but `BaseDialog` real. Open it, close it through the shared dialog close button, and assert:

```ts
expect(appRoot.hasAttribute('inert')).toBe(true)
expect(document.body.style.overflow).toBe('hidden')

await wrapper.getComponent(BaseDialog).vm.$emit('close')
await wrapper.setProps({ open: false })
await nextTick()

expect(wrapper.emitted('close')).toHaveLength(1)
expect(appRoot.hasAttribute('inert')).toBe(false)
expect(document.body.style.overflow).toBe('')
expect(trigger.matches(':focus')).toBe(true)
```

Add rapid lifecycle coverage:

```ts
await wrapper.setProps({ open: false })
await wrapper.setProps({ open: true })
await wrapper.setProps({ open: false })
wrapper.unmount()

expect(appRoot.hasAttribute('inert')).toBe(false)
expect(document.body.style.overflow).toBe('')
```

The test must use the actual `BaseDialog`, not a stub that bypasses its watcher.

- [ ] **Step 2: Run drawer and modal tests and verify RED**

Run:

```bash
pnpm vitest run src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts src/utils/__tests__/modalBodyLock.spec.ts --reporter=verbose
```

Expected: FAIL because the drawer does not yet contain `BaseDialog` and still owns global lifecycle state.

- [ ] **Step 3: Replace the shell while preserving investigation content**

In `PerformanceInvestigationDrawer.vue`:

1. Remove `nextTick`, `onMounted`, `onUnmounted`, `ref`, and `watch` imports.
2. Remove all direct imports from `modalBodyLock`.
3. Remove the manual keydown handlers, focusable selector, lock flags, and restore methods.
4. Import `BaseDialog` and `PlatformTypeBadge`.
5. Add a title computed value:

```ts
const dialogTitle = computed(() => {
  if (!props.account) return '账号性能详情'
  const name = props.account.account_name || `#${props.account.account_id}`
  return `${name} · #${props.account.account_id}`
})
```

Wrap the existing content:

```vue
<BaseDialog
  :show="open"
  :title="dialogTitle"
  width="extra-wide"
  :close-on-click-outside="true"
  @close="emit('close')"
>
  <div v-if="account" class="mb-5 flex items-center justify-between gap-3 border-b border-gray-200 pb-4 dark:border-dark-700">
    <PlatformTypeBadge
      v-if="account.account_type"
      :platform="account.platform"
      :type="account.account_type"
      :auth-mode="account.auth_mode"
    />
  </div>
  <!-- retain metric cards, loading, error/retry, trend, and failure distribution -->
</BaseDialog>
```

Keep `eligibleAttempts` and `failureCount`; they already implement the canonical non-cancelled failure definition.

- [ ] **Step 4: Adapt existing drawer assertions to the shared dialog**

Use `wrapper.getComponent(BaseDialog).vm.$emit('close')` instead of querying the removed `performance-investigation-close` button. Retain assertions for loading, empty, retry, failure context, overlapping dialogs, and cleanup. Assert the title contains the account name and `#42`.

- [ ] **Step 5: Add one real parent-view selection and cleanup test**

Mount `AccountPerformanceView` with the real drawer and `BaseDialog`, while stubbing only charts and metric visuals. Select the account, wait for investigation, emit close from `BaseDialog`, and assert the page's refresh button can receive a click and `#app` is not inert. This covers the production component boundary missing from current unit tests.

Also add `account_name`, `account_type`, and `auth_mode` to the drawer fixture in `modalBodyLock.spec.ts`. Do not change its reference-counting expectations.

- [ ] **Step 6: Run focused modal tests and verify GREEN**

Run:

```bash
pnpm vitest run src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts src/utils/__tests__/modalBodyLock.spec.ts --reporter=verbose
```

Expected: PASS with no residual `inert` attribute or body scroll lock after close/unmount.

- [ ] **Step 7: Commit the dialog fix**

```bash
git add frontend/src/views/admin/performance/components/PerformanceInvestigationDrawer.vue frontend/src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts frontend/src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts frontend/src/utils/__tests__/modalBodyLock.spec.ts
git commit -m "fix: stabilize account performance details"
```

### Task 4: Full Verification and Completion Audit

**Files:**
- Verify all files changed by Tasks 1-3.

- [ ] **Step 1: Format changed source files**

Run:

```bash
gofmt -w backend/internal/service/account_performance.go backend/internal/repository/account_performance_repo.go backend/internal/repository/account_performance_repo_test.go backend/internal/repository/account_performance_repo_integration_test.go
pnpm exec prettier --write src/api/admin/performance.ts src/views/admin/performance/components/PerformanceAccountTable.vue src/views/admin/performance/components/PerformanceInvestigationDrawer.vue src/views/admin/performance/components/__tests__/PerformanceAccountTable.spec.ts src/views/admin/performance/components/__tests__/PerformanceInvestigationDrawer.spec.ts src/views/admin/performance/__tests__/AccountPerformanceView.spec.ts
```

Run the frontend formatter from `frontend/`. Review the diff afterward so formatting does not churn unrelated files.

- [ ] **Step 2: Run backend verification**

From `backend/`:

```bash
go test ./internal/repository ./internal/service ./internal/handler/admin -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

From `frontend/`:

```bash
pnpm vitest run src/views/admin/performance src/utils/__tests__/modalBodyLock.spec.ts --reporter=verbose
pnpm type-check
pnpm build
```

Expected: all tests PASS, type checking exits 0, and the production bundle completes.

- [ ] **Step 4: Inspect the rendered page**

Run the existing local stack or frontend dev server. At desktop and mobile widths, verify:

```text
account name + #ID visible
platform/type badge matches Accounts page
successful and failed call counts use the confirmed formula
table scrolls horizontally without overlap
detail opens as a wide dialog
close, Escape, outside click, and rapid reopen all restore page interaction
light and dark themes remain readable
```

Capture console errors and fail the verification if opening or closing the dialog logs an uncaught Vue/Chart.js exception.

- [ ] **Step 5: Review the final diff and working tree**

Run:

```bash
git diff --check
git status --short
git log -5 --oneline
```

Expected: no whitespace errors; only the user's pre-existing untracked `api_key` may remain.

- [ ] **Step 6: Commit any verification-only adjustments**

If formatting or verification required tracked changes, stage only the task files and commit:

```bash
git commit -m "test: verify account performance details"
```

Do not add `api_key` or `.superpowers/` artifacts.
