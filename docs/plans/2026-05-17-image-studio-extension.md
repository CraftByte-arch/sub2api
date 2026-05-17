# Image Studio Extension Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a native Image Studio user page as a low-conflict fork extension.

**Architecture:** Build the feature under `frontend/src/extensions/image-studio/` and call the existing Sub2API gateway endpoints from the browser using a selected user API key. Keep upstream-owned file edits limited to route registration, sidebar entry, and i18n labels.

**Tech Stack:** Vue 3, TypeScript, Vue Router, Pinia, Axios, Vitest, TailwindCSS-style existing classes.

---

### Task 1: Extension Route Registry

**Files:**
- Create: `frontend/src/extensions/index.ts`
- Modify: `frontend/src/router/index.ts`
- Test: `frontend/src/router/__tests__/guards.spec.ts` or a new focused router test if cleaner

**Step 1: Write the failing test**

Add a test that asserts `/image-studio` resolves to an authenticated non-admin route named `ImageStudio`.

**Step 2: Run the test to verify it fails**

Run:

```bash
pnpm -C frontend test:run frontend/src/router/__tests__/guards.spec.ts
```

Expected: FAIL because the route does not exist.

**Step 3: Add the extension registry**

Create `frontend/src/extensions/index.ts` exporting:

```ts
import type { RouteRecordRaw } from 'vue-router'

export const extensionRoutes: RouteRecordRaw[] = [
  {
    path: '/image-studio',
    name: 'ImageStudio',
    component: () => import('@/extensions/image-studio/ImageStudioView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Image Studio',
      titleKey: 'imageStudio.title',
      descriptionKey: 'imageStudio.description'
    }
  }
]
```

Modify `frontend/src/router/index.ts` to import `extensionRoutes` and spread them into the user routes section.

**Step 4: Run the test to verify it passes**

Run:

```bash
pnpm -C frontend test:run frontend/src/router/__tests__/guards.spec.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/extensions/index.ts frontend/src/router/index.ts frontend/src/router/__tests__/guards.spec.ts
git commit -m "feat(extension): register image studio route"
```

### Task 2: Sidebar Entry

**Files:**
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Test: existing sidebar/nav tests if present, otherwise add a small component test

**Step 1: Write the failing test**

Assert that the user sidebar includes an `Image Studio`/`生图工作台` navigation item pointing to `/image-studio`.

**Step 2: Run the test to verify it fails**

Run:

```bash
pnpm -C frontend test:run
```

Expected: FAIL on missing navigation item.

**Step 3: Add nav item**

In `buildSelfNavItems`, add a user-facing item near `Available Channels`:

```ts
{ path: '/image-studio', label: t('nav.imageStudio'), icon: ImageIcon, hideInSimpleMode: true }
```

Use an existing icon component if available. If no image icon exists, add it through the existing icon pattern in `AppSidebar.vue`.

Add translations:

```ts
nav: {
  imageStudio: '生图工作台'
}
```

```ts
nav: {
  imageStudio: 'Image Studio'
}
```

Add page translations:

```ts
imageStudio: {
  title: '生图工作台',
  description: '使用当前账号的 API Key 调用 Sub2API 生图能力。'
}
```

```ts
imageStudio: {
  title: 'Image Studio',
  description: 'Use your API keys to generate and edit images through Sub2API.'
}
```

**Step 4: Run the test to verify it passes**

Run:

```bash
pnpm -C frontend test:run
```

Expected: PASS for affected tests.

**Step 5: Commit**

```bash
git add frontend/src/components/layout/AppSidebar.vue frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts
git commit -m "feat(extension): add image studio navigation"
```

### Task 3: Payload Builders

**Files:**
- Create: `frontend/src/extensions/image-studio/types.ts`
- Create: `frontend/src/extensions/image-studio/payload.ts`
- Create: `frontend/src/extensions/image-studio/__tests__/payload.spec.ts`

**Step 1: Write failing tests**

Cover:

- Responses generation payload includes `tools: [{ type: 'image_generation' }]`
- Images API generation payload includes `model`, `prompt`, `size`, and optional controls
- edit payload can include input image data URL for Responses mode
- invalid advanced JSON is rejected by caller-facing parser

**Step 2: Run tests to verify they fail**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/payload.spec.ts
```

Expected: FAIL because files do not exist.

**Step 3: Implement payload builders**

Create typed helpers:

```ts
export function buildResponsesGenerationPayload(input: ImageStudioGenerationInput): Record<string, unknown>
export function buildImagesGenerationPayload(input: ImageStudioGenerationInput): Record<string, unknown>
export function buildResponsesEditPayload(input: ImageStudioEditInput): Promise<Record<string, unknown>>
export function buildImagesEditFormData(input: ImageStudioEditInput): FormData
```

Keep these helpers pure where possible.

**Step 4: Run tests to verify they pass**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/payload.spec.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/extensions/image-studio/types.ts frontend/src/extensions/image-studio/payload.ts frontend/src/extensions/image-studio/__tests__/payload.spec.ts
git commit -m "feat(extension): add image studio payload builders"
```

### Task 4: Output Extraction

**Files:**
- Create: `frontend/src/extensions/image-studio/output.ts`
- Create: `frontend/src/extensions/image-studio/__tests__/output.spec.ts`

**Step 1: Write failing tests**

Cover extraction from:

- Images API `data[].b64_json`
- Images API `data[].url`
- Responses `output[]` image generation call with `result`
- Responses stream-aggregated shape if returned by compatible upstream

**Step 2: Run tests to verify they fail**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/output.spec.ts
```

Expected: FAIL.

**Step 3: Implement extraction helpers**

Create:

```ts
export function extractImageStudioOutputs(response: unknown): ImageStudioOutput[]
```

Return normalized output objects with `src`, `kind`, `mimeType`, `revisedPrompt`, and `raw`.

**Step 4: Run tests to verify they pass**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/output.spec.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/extensions/image-studio/output.ts frontend/src/extensions/image-studio/__tests__/output.spec.ts
git commit -m "feat(extension): normalize image studio outputs"
```

### Task 5: Gateway API Client

**Files:**
- Create: `frontend/src/extensions/image-studio/imageStudioApi.ts`
- Test: `frontend/src/extensions/image-studio/__tests__/imageStudioApi.spec.ts`

**Step 1: Write failing tests**

Mock Axios/fetch and assert:

- `/v1/responses` request uses selected API key in `Authorization`
- `/v1/images/generations` JSON request uses selected API key
- `/v1/images/edits` multipart request uses selected API key and does not force JSON content type

**Step 2: Run tests to verify they fail**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/imageStudioApi.spec.ts
```

Expected: FAIL.

**Step 3: Implement client**

Use a dedicated Axios instance or direct `fetch` for gateway calls because `/v1/...` is outside `/api/v1` and should not be unwrapped by `apiClient`.

**Step 4: Run tests to verify they pass**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/imageStudioApi.spec.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/extensions/image-studio/imageStudioApi.ts frontend/src/extensions/image-studio/__tests__/imageStudioApi.spec.ts
git commit -m "feat(extension): add image studio gateway client"
```

### Task 6: Image Studio Page

**Files:**
- Create: `frontend/src/extensions/image-studio/ImageStudioView.vue`
- Test: `frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts`

**Step 1: Write failing component tests**

Cover:

- renders loading state
- lists active API keys
- switches between text-to-image and edit modes
- disables submit when no API key or prompt is selected
- renders extracted image outputs after a mocked successful request

**Step 2: Run tests to verify they fail**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
```

Expected: FAIL.

**Step 3: Implement page**

Use a workspace layout close to the provided reference screenshot:

- keep the normal Sub2API app sidebar visible
- use one large rounded workspace shell in the main content area
- put `文生图`, `图生图`, and `记录` tabs in the workspace top bar
- use teal text and underline for the active tab
- split the workspace body into a left control column and right preview canvas
- keep the left column about 500-540px on desktop
- make the right canvas a dashed-border empty/result area
- render aspect ratio choices as fixed-size selectable tiles
- render the image count as a slider with the current value aligned right
- make the prompt textarea visually prominent, with teal focus treatment
- show the empty state text `还没有图片` and `在左侧填写提示词和参数，点击生成图片。`
- use a single-column layout on mobile where controls come before preview
- keep form controls and result tiles stable in size
- use accessible labels and buttons

Load keys with `keysAPI.list`. Filter active keys first and visibly mark groups that appear image-capable.

The first implementation should support the visual shape even if the `记录` tab is initially a local/session history placeholder. Do not add backend history storage in this task.

**Step 4: Run component tests**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/extensions/image-studio/ImageStudioView.vue frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
git commit -m "feat(extension): build image studio page"
```

### Task 7: Verification

**Files:**
- No new files unless tests reveal necessary fixes.

**Step 1: Run targeted tests**

Run:

```bash
pnpm -C frontend test:run frontend/src/extensions/image-studio
```

Expected: PASS.

**Step 2: Run typecheck**

Run:

```bash
pnpm -C frontend typecheck
```

Expected: PASS.

**Step 3: Run frontend dev server**

Run:

```bash
pnpm -C frontend dev
```

Expected: Vite starts successfully.

**Step 4: Browser verification**

Open the local Vite URL with the in-app Browser and verify:

- `/image-studio` loads after login
- sidebar item is visible
- form controls do not overlap on desktop or mobile width
- mocked or real gateway call result renders image output

**Step 5: Final commit if fixes were needed**

```bash
git add frontend/src
git commit -m "test(extension): verify image studio integration"
```
