---
change: configurable-homepage-variant
design-doc: docs/superpowers/specs/2026-07-06-configurable-homepage-variant-design.md
base-ref: 36e03e9fb7241dc861865b43088e2bc90804713c
---

# Configurable Homepage Variant Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let container deployments choose the `/home` page with `HOMEPAGE_VARIANT=default|aixw`, defaulting to the upstream homepage.

**Architecture:** Keep the change small. Backend normalizes one environment variable and exposes it through existing public settings and HTML injection. Frontend routes `/home` to one thin selector component that renders either `HomeView.vue` or `AixwHomeView.vue`.

**Tech Stack:** Go service/settings DTOs, Gin handler public settings, Vue 3, Pinia, Vue Router, Vitest.

---

## File Structure

- Modify `backend/internal/service/settings_view.go`: add `HomepageVariant` to service public settings.
- Modify `backend/internal/service/setting_service.go`: add normalization helper, populate public settings, and include `homepage_variant` in injection payload.
- Modify `backend/internal/handler/dto/settings.go`: add `homepage_variant` to public settings DTO.
- Modify `backend/internal/handler/setting_handler.go`: copy `settings.HomepageVariant` into the public response.
- Modify backend tests in `backend/internal/service/setting_service_public_test.go` and `backend/internal/handler/setting_handler_public_test.go`.
- Modify `frontend/src/types/index.ts`: add `homepage_variant?: 'default' | 'aixw' | string`.
- Create `frontend/src/views/public/HomeVariantView.vue`: thin selector component.
- Modify `frontend/src/router/index.ts`: route `/home` to `HomeVariantView.vue`.
- Add frontend tests for selector behavior and update `frontend/src/__tests__/router/home-route.spec.ts`.
- Modify `deploy/.env.example`: document `HOMEPAGE_VARIANT=default`.

### Task 1: Backend Runtime Setting

**Files:**
- Modify: `backend/internal/service/settings_view.go`
- Modify: `backend/internal/service/setting_service.go`
- Modify: `backend/internal/handler/dto/settings.go`
- Modify: `backend/internal/handler/setting_handler.go`
- Test: `backend/internal/service/setting_service_public_test.go`
- Test: `backend/internal/handler/setting_handler_public_test.go`

- [x] **Step 1: Add failing service tests**

Add tests like:

```go
func TestSettingService_GetPublicSettings_HomepageVariantDefaultsToDefault(t *testing.T) {
	t.Setenv("HOMEPAGE_VARIANT", "")
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{}}, &config.Config{})

	settings, err := svc.GetPublicSettings(context.Background())

	require.NoError(t, err)
	require.Equal(t, "default", settings.HomepageVariant)
}

func TestSettingService_GetPublicSettings_HomepageVariantReadsAixw(t *testing.T) {
	t.Setenv("HOMEPAGE_VARIANT", "aixw")
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{}}, &config.Config{})

	settings, err := svc.GetPublicSettings(context.Background())

	require.NoError(t, err)
	require.Equal(t, "aixw", settings.HomepageVariant)
}

func TestSettingService_GetPublicSettings_HomepageVariantInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("HOMEPAGE_VARIANT", "surprise")
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{}}, &config.Config{})

	settings, err := svc.GetPublicSettings(context.Background())

	require.NoError(t, err)
	require.Equal(t, "default", settings.HomepageVariant)
}
```

- [x] **Step 2: Run service tests and verify failure**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'TestSettingService_GetPublicSettings_HomepageVariant' -count=1
```

Expected: FAIL because `HomepageVariant` does not exist yet.

- [x] **Step 3: Implement minimal backend service support**

Add constants/helper near public settings code in `setting_service.go`:

```go
const (
	HomepageVariantDefault = "default"
	HomepageVariantAixw    = "aixw"
	homepageVariantEnvKey  = "HOMEPAGE_VARIANT"
)

func normalizeHomepageVariant(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case HomepageVariantAixw:
		return HomepageVariantAixw
	default:
		return HomepageVariantDefault
	}
}

func homepageVariantFromEnv() string {
	return normalizeHomepageVariant(os.Getenv(homepageVariantEnvKey))
}
```

Add `HomepageVariant string` to `service.PublicSettings`, set `HomepageVariant: homepageVariantFromEnv()` in `GetPublicSettings`, and import `os` if needed.

- [x] **Step 4: Expose public response and injection payload**

Add `HomepageVariant string ` + "`json:\"homepage_variant\"`" + ` to `dto.PublicSettings` and `service.PublicSettingsInjectionPayload`.

Copy the field in:

```go
HomepageVariant: settings.HomepageVariant,
```

inside both `SettingHandler.GetPublicSettings` response construction and `GetPublicSettingsForInjection`.

- [x] **Step 5: Add focused handler/injection tests**

In `setting_handler_public_test.go`, add a handler test with `t.Setenv("HOMEPAGE_VARIANT", "aixw")` and assert `resp.Data.HomepageVariant == "aixw"`.

Rely on existing `TestPublicSettingsInjectionPayload_SchemaDoesNotDrift` to catch DTO/injection schema mismatch, and add a direct assertion in a service test:

```go
payload, err := svc.GetPublicSettingsForInjection(context.Background())
require.NoError(t, err)
require.Equal(t, "aixw", payload.(*PublicSettingsInjectionPayload).HomepageVariant)
```

- [x] **Step 6: Run backend focused tests**

Run:

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler ./internal/handler/dto -run 'HomepageVariant|PublicSettingsInjectionPayload' -count=1
```

Expected: PASS.

### Task 2: Frontend Homepage Selector

**Files:**
- Modify: `frontend/src/types/index.ts`
- Create: `frontend/src/views/public/HomeVariantView.vue`
- Modify: `frontend/src/router/index.ts`
- Test: `frontend/src/__tests__/router/home-route.spec.ts`
- Test: `frontend/src/__tests__/views/public/HomeVariantView.spec.ts`

- [x] **Step 1: Add failing selector tests**

Create `frontend/src/__tests__/views/public/HomeVariantView.spec.ts`.

Use shallow stubs for `HomeView` and `AixwHomeView`, seed Pinia public settings, and assert:

```ts
expect(wrapper.find('[data-testid="default-home-stub"]').exists()).toBe(true)
expect(wrapper.find('[data-testid="aixw-home-stub"]').exists()).toBe(false)
```

for missing/default settings, and the reverse for `{ homepage_variant: 'aixw' }`.

- [x] **Step 2: Run selector test and verify failure**

Run:

```bash
cd frontend && pnpm test:run src/__tests__/views/public/HomeVariantView.spec.ts
```

Expected: FAIL because `HomeVariantView.vue` does not exist.

- [x] **Step 3: Add frontend type and selector component**

Add to `PublicSettings` in `frontend/src/types/index.ts`:

```ts
homepage_variant?: 'default' | 'aixw' | string
```

Create `frontend/src/views/public/HomeVariantView.vue`:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores'
import HomeView from '@/views/HomeView.vue'
import AixwHomeView from '@/views/public/AixwHomeView.vue'

const appStore = useAppStore()
const variant = computed(() =>
  appStore.cachedPublicSettings?.homepage_variant === 'aixw' ? 'aixw' : 'default'
)
</script>

<template>
  <AixwHomeView v-if="variant === 'aixw'" />
  <HomeView v-else />
</template>
```

- [x] **Step 4: Route `/home` through selector**

In `frontend/src/router/index.ts`, change only the `/home` route component import to:

```ts
component: () => import('@/views/public/HomeVariantView.vue'),
```

Keep path, name, and meta unchanged.

- [x] **Step 5: Update route test**

Update `frontend/src/__tests__/router/home-route.spec.ts` to assert `/home` still resolves to name `Home`, has a matched route, and its component async import resolves to `HomeVariantView`.

- [x] **Step 6: Run frontend focused tests**

Run:

```bash
cd frontend && pnpm test:run src/__tests__/views/public/HomeVariantView.spec.ts src/__tests__/router/home-route.spec.ts
```

Expected: PASS.

### Task 3: Documentation And Final Verification

**Files:**
- Modify: `deploy/.env.example`
- Modify: `openspec/changes/configurable-homepage-variant/tasks.md`

- [x] **Step 1: Document the environment variable**

Add a small commented block to `deploy/.env.example` near other frontend/server runtime options:

```env
# Public homepage variant: default = upstream homepage, aixw = custom AIXW homepage
HOMEPAGE_VARIANT=default
```

- [x] **Step 2: Run final focused verification**

Run:

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler ./internal/handler/dto -run 'HomepageVariant|PublicSettingsInjectionPayload' -count=1
cd frontend && pnpm test:run src/__tests__/views/public/HomeVariantView.spec.ts src/__tests__/router/home-route.spec.ts
cd frontend && pnpm typecheck
```

Expected: all commands pass.

- [x] **Step 3: Update OpenSpec tasks**

After implementation and verification pass, mark completed items in `openspec/changes/configurable-homepage-variant/tasks.md`.

- [ ] **Step 4: Commit**

Commit the implementation with:

```bash
git add backend/internal/service/settings_view.go backend/internal/service/setting_service.go backend/internal/handler/dto/settings.go backend/internal/handler/setting_handler.go backend/internal/service/setting_service_public_test.go backend/internal/handler/setting_handler_public_test.go frontend/src/types/index.ts frontend/src/views/public/HomeVariantView.vue frontend/src/router/index.ts frontend/src/__tests__/router/home-route.spec.ts frontend/src/__tests__/views/public/HomeVariantView.spec.ts deploy/.env.example openspec/changes/configurable-homepage-variant/tasks.md
git commit -m "feat: make homepage variant runtime configurable"
```

## Self-Review

- Spec coverage: tasks cover environment normalization, public settings exposure, HTML injection exposure, frontend selection, missing/invalid default behavior, and Docker documentation.
- Placeholder scan: no TBD/TODO placeholders.
- Type consistency: backend and frontend both use JSON field `homepage_variant`; accepted values are `default` and `aixw`.
