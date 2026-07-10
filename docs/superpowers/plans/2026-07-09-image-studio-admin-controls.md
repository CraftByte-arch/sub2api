---
change: configure-image-studio-available-groups
design-doc: docs/superpowers/specs/2026-07-09-image-studio-admin-controls-design.md
base-ref: f5cd222e59e2e576fe4c5e751e3fd240687a0377
---

# Image Studio Admin Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add admin controls for image studio available groups and client `image_generation` tool declaration policy.

**Architecture:** Store both controls in the existing system settings pipeline. Enforce image studio group availability in the image studio job path and frontend key selector. Apply the tool declaration policy in OpenAI Responses HTTP and WebSocket ingress before existing actual-image-generation gates.

**Tech Stack:** Go backend with Gin handlers and service settings; Vue 3 + TypeScript frontend; Vitest frontend tests; Go unit/handler tests.

---

## File Structure

- Modify `backend/internal/service/domain_constants.go`: add setting keys and policy constants.
- Modify `backend/internal/service/settings_view.go`: add fields to `SystemSettings`.
- Modify `backend/internal/service/setting_service.go`: parse defaults, normalize writes, and persist new settings.
- Modify `backend/internal/handler/dto/settings.go`: expose settings DTO fields.
- Modify `backend/internal/handler/admin/setting_handler.go`: bind request fields, validate policy, include response/audit fields.
- Modify `backend/internal/service/image_generation_intent.go`: split passive declaration detection from actual image-generation intent.
- Modify `backend/internal/service/openai_gateway_service.go`: apply declaration policy to HTTP `/v1/responses` gateway path.
- Modify `backend/internal/service/openai_ws_forwarder.go`: apply declaration policy to Responses WebSocket ingress.
- Modify `backend/internal/handler/image_studio_job_handler.go` and `backend/internal/service/image_studio_job_worker.go`: enforce image studio allowlist.
- Modify `frontend/src/api/admin/settings.ts`: add TypeScript fields.
- Modify `frontend/src/views/admin/SettingsView.vue`: add controls, form defaults, payload serialization.
- Modify `frontend/src/extensions/image-studio/ImageStudioView.vue`: fetch settings and filter API keys by allowlist.
- Modify `frontend/src/i18n/locales/zh.ts` and `frontend/src/i18n/locales/en.ts`: add labels/help text.
- Add/update backend tests near existing settings, gateway, WS, and image studio tests.
- Add/update frontend tests under `frontend/src/extensions/image-studio/__tests__/` and settings tests if present.

---

### Task 1: Backend settings schema and normalization

**Files:**
- Modify: `backend/internal/service/domain_constants.go`
- Modify: `backend/internal/service/settings_view.go`
- Modify: `backend/internal/service/setting_service.go`
- Modify: `backend/internal/handler/dto/settings.go`
- Modify: `backend/internal/handler/admin/setting_handler.go`
- Test: existing backend settings tests under `backend/internal/service/*setting*_test.go` and `backend/internal/handler/admin/*setting*_test.go`

- [x] **Step 1: Write failing backend settings tests**

Add tests that assert default settings include:

```go
ImageStudioAvailableGroupIDs: []int64{}
ImageGenerationToolDeclarationPolicy: "strip"
```

Add an update/read test with payload equivalent to:

```json
{
  "image_studio_available_group_ids": [3, 2, 3, 0, -1],
  "image_generation_tool_declaration_policy": "allow"
}
```

Expected readback should contain unique positive IDs in stable order and policy `allow`:

```json
{
  "image_studio_available_group_ids": [3, 2],
  "image_generation_tool_declaration_policy": "allow"
}
```

Run targeted tests:

```bash
cd backend && go test ./internal/service ./internal/handler/admin -run 'ImageStudio|Settings|DeclarationPolicy' -count=1
```

Expected now: FAIL because fields do not exist.

- [x] **Step 2: Add setting constants and normalization helpers**

In `backend/internal/service/domain_constants.go`, add:

```go
SettingKeyImageStudioAvailableGroupIDs = "image_studio_available_group_ids"
SettingKeyImageGenerationToolDeclarationPolicy = "image_generation_tool_declaration_policy"

ImageGenerationToolDeclarationPolicyStrip = "strip"
ImageGenerationToolDeclarationPolicyAllow = "allow"
ImageGenerationToolDeclarationPolicyReject = "reject"
```

In `backend/internal/service/setting_service.go`, add helpers:

```go
func NormalizeImageGenerationToolDeclarationPolicy(value string) string {
    switch strings.ToLower(strings.TrimSpace(value)) {
    case ImageGenerationToolDeclarationPolicyAllow:
        return ImageGenerationToolDeclarationPolicyAllow
    case ImageGenerationToolDeclarationPolicyReject:
        return ImageGenerationToolDeclarationPolicyReject
    default:
        return ImageGenerationToolDeclarationPolicyStrip
    }
}

func normalizeImageStudioAvailableGroupIDs(ids []int64) []int64 {
    seen := make(map[int64]struct{}, len(ids))
    out := make([]int64, 0, len(ids))
    for _, id := range ids {
        if id <= 0 {
            continue
        }
        if _, ok := seen[id]; ok {
            continue
        }
        seen[id] = struct{}{}
        out = append(out, id)
    }
    return out
}
```

Use existing JSON setting parse/write helpers if the file already has a list-setting pattern; otherwise marshal/unmarshal `[]int64` as JSON string in settings storage.

- [x] **Step 3: Add fields to service and DTO structs**

Add to `backend/internal/service/settings_view.go` `SystemSettings`:

```go
ImageStudioAvailableGroupIDs []int64
ImageGenerationToolDeclarationPolicy string
```

Add to `backend/internal/handler/dto/settings.go` and `backend/internal/handler/admin/setting_handler.go` request/response structs:

```go
ImageStudioAvailableGroupIDs []int64 `json:"image_studio_available_group_ids"`
ImageGenerationToolDeclarationPolicy string `json:"image_generation_tool_declaration_policy"`
```

- [x] **Step 4: Wire defaults, reads, writes, response, and audit**

In default settings map, add:

```go
SettingKeyImageStudioAvailableGroupIDs: "[]",
SettingKeyImageGenerationToolDeclarationPolicy: ImageGenerationToolDeclarationPolicyStrip,
```

In settings parsing, set:

```go
ImageStudioAvailableGroupIDs: parseInt64ListSetting(settings[SettingKeyImageStudioAvailableGroupIDs]),
ImageGenerationToolDeclarationPolicy: NormalizeImageGenerationToolDeclarationPolicy(settings[SettingKeyImageGenerationToolDeclarationPolicy]),
```

In update path, persist:

```go
updates[SettingKeyImageStudioAvailableGroupIDs] = marshalInt64ListSetting(normalizeImageStudioAvailableGroupIDs(settings.ImageStudioAvailableGroupIDs))
updates[SettingKeyImageGenerationToolDeclarationPolicy] = NormalizeImageGenerationToolDeclarationPolicy(settings.ImageGenerationToolDeclarationPolicy)
```

In admin handler response and changed-field audit, include both setting keys.

- [x] **Step 5: Run tests and commit**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler/admin -run 'ImageStudio|Settings|DeclarationPolicy' -count=1
```

Expected: PASS.

Commit:

```bash
git add backend/internal/service/domain_constants.go backend/internal/service/settings_view.go backend/internal/service/setting_service.go backend/internal/handler/dto/settings.go backend/internal/handler/admin/setting_handler.go backend/internal/service/*setting*_test.go backend/internal/handler/admin/*setting*_test.go
git commit -m "feat: add image studio admin settings"
```

---

### Task 2: Gateway declaration policy and intent split

**Files:**
- Modify: `backend/internal/service/image_generation_intent.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_ws_forwarder.go`
- Test: `backend/internal/service/openai_gateway_service_hotpath_test.go`
- Test: `backend/internal/service/openai_ws_forwarder*_test.go`

- [x] **Step 1: Write failing tests for passive declaration**

Add tests for a disabled image-generation group and body:

```json
{
  "model": "gpt-5.4",
  "input": "hello",
  "tools": [{"type":"image_generation","output_format":"png"}],
  "tool_choice": "auto"
}
```

Expected behavior:

- policy `strip`: request is not rejected for `Image generation is not enabled for this group`; forwarded body has no `image_generation` tool.
- policy `allow`: request is not rejected for `Image generation is not enabled for this group`; forwarded body still contains the tool.
- policy `reject`: request is rejected.

Add a separate test for explicit choice:

```json
{"tool_choice":{"type":"image_generation"}}
```

Expected: still rejected when group image generation is disabled.

Run:

```bash
cd backend && go test ./internal/service -run 'ImageGeneration|DeclarationPolicy|Gateway|WS' -count=1
```

Expected now: FAIL because passive declaration is still treated as actual image intent.

- [x] **Step 2: Split passive declaration and actual image intent helpers**

In `backend/internal/service/image_generation_intent.go`, add helpers like:

```go
func HasPassiveImageGenerationToolDeclaration(endpoint string, requestedModel string, body []byte) bool {
    if IsImageGenerationEndpoint(endpoint) || isOpenAIImageGenerationModel(requestedModel) {
        return false
    }
    if len(body) == 0 || !gjson.ValidBytes(body) {
        return false
    }
    if isOpenAIImageGenerationModel(gjson.GetBytes(body, "model").String()) {
        return false
    }
    if !openAIJSONToolsContainImageGeneration(gjson.GetBytes(body, "tools")) {
        return false
    }
    return !openAIJSONToolChoiceSelectsImageGeneration(gjson.GetBytes(body, "tool_choice"))
}

func IsActualImageGenerationIntent(endpoint string, requestedModel string, body []byte) bool {
    if IsImageGenerationEndpoint(endpoint) || isOpenAIImageGenerationModel(requestedModel) {
        return true
    }
    if len(body) == 0 || !gjson.ValidBytes(body) {
        return false
    }
    if isOpenAIImageGenerationModel(gjson.GetBytes(body, "model").String()) {
        return true
    }
    return openAIJSONToolChoiceSelectsImageGeneration(gjson.GetBytes(body, "tool_choice"))
}
```

Keep the old `IsImageGenerationIntent` for dedicated billing/config paths if needed, but update permission gates to use actual intent plus declaration policy.

- [x] **Step 3: Apply HTTP declaration policy before permission gate**

In `backend/internal/service/openai_gateway_service.go`, before the current disabled-group image-generation check, load settings and apply:

```go
policy := ImageGenerationToolDeclarationPolicyStrip
if s.settingService != nil {
    if settings, err := s.settingService.GetSettings(ctx); err == nil && settings != nil {
        policy = NormalizeImageGenerationToolDeclarationPolicy(settings.ImageGenerationToolDeclarationPolicy)
    }
}

if HasPassiveImageGenerationToolDeclaration(openAIResponsesEndpoint, reqModel, body) {
    switch policy {
    case ImageGenerationToolDeclarationPolicyReject:
        c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "permission_error", "message": "image_generation tool declaration is disabled"}})
        return nil, errors.New("image generation tool declaration disabled")
    case ImageGenerationToolDeclarationPolicyStrip:
        decoded, decodeErr := ensureReqBody()
        if decodeErr != nil { return nil, decodeErr }
        if stripOpenAIImageGenerationTools(decoded) {
            markDecodedModified()
        }
    }
}
```

Then use actual intent for disabled group checks.

- [x] **Step 4: Apply the same policy in Responses WebSocket ingress**

In `backend/internal/service/openai_ws_forwarder.go`, apply the same policy to normalized incoming payload before the disabled-group gate. Reuse raw-payload strip helpers if available; otherwise decode, strip, and re-marshal consistently.

- [x] **Step 5: Run tests and commit**

Run:

```bash
cd backend && go test ./internal/service -run 'ImageGeneration|DeclarationPolicy|Gateway|WS' -count=1
```

Expected: PASS.

Commit:

```bash
git add backend/internal/service/image_generation_intent.go backend/internal/service/openai_gateway_service.go backend/internal/service/openai_ws_forwarder.go backend/internal/service/*test.go
git commit -m "fix: separate image tool declaration from image generation intent"
```

---

### Task 3: Image studio group allowlist enforcement

**Files:**
- Modify: `backend/internal/handler/image_studio_job_handler.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Test: existing image studio handler/service tests under `backend/internal/handler/*image_studio*_test.go` and `backend/internal/service/*image_studio*_test.go`

- [x] **Step 1: Write failing tests**

Add handler test cases:

- settings allowlist contains the key group ID: job creation succeeds.
- settings allowlist is empty: job creation rejects.
- settings allowlist does not contain key group ID: job creation rejects with a clear message.

Expected message text:

```text
API key group is not available for image studio
```

Run:

```bash
cd backend && go test ./internal/handler ./internal/service -run 'ImageStudio.*Group|ImageStudio.*Available' -count=1
```

Expected now: FAIL because allowlist is not enforced.

- [x] **Step 2: Add allowlist helper**

Add a helper in the service layer or image studio handler file:

```go
func ImageStudioGroupAllowed(groupID *int64, allowedIDs []int64) bool {
    if groupID == nil || *groupID <= 0 {
        return false
    }
    for _, id := range allowedIDs {
        if id == *groupID {
            return true
        }
    }
    return false
}
```

Use the actual group ID type already used by `APIKey.GroupID`.

- [x] **Step 3: Enforce in job creation handler**

In `backend/internal/handler/image_studio_job_handler.go`, after existing active key and group image-generation checks, load settings and check:

```go
settings, err := h.settingService.GetSettings(c.Request.Context())
if err != nil {
    response.ErrorFrom(c, err)
    return
}
if !service.ImageStudioGroupAllowed(apiKey.GroupID, settings.ImageStudioAvailableGroupIDs) {
    response.Forbidden(c, "API key group is not available for image studio")
    return
}
```

If the handler does not currently have `settingService`, add it through the constructor/wire path following existing dependency injection patterns.

- [x] **Step 4: Re-check in worker if practical**

In `backend/internal/service/image_studio_job_worker.go`, after `GroupAllowsImageGeneration`, add a safe setting check. If settings cannot be read in the worker, fail the job with an internal error; if group not allowed, fail with code:

```go
"image_studio_group_unavailable"
```

- [x] **Step 5: Run tests and commit**

Run:

```bash
cd backend && go test ./internal/handler ./internal/service -run 'ImageStudio.*Group|ImageStudio.*Available' -count=1
```

Expected: PASS.

Commit:

```bash
git add backend/internal/handler/image_studio_job_handler.go backend/internal/service/image_studio_job_worker.go backend/internal/handler/*image_studio*_test.go backend/internal/service/*image_studio*_test.go
git commit -m "feat: enforce image studio group allowlist"
```

---

### Task 4: Admin settings UI controls

**Files:**
- Modify: `frontend/src/api/admin/settings.ts`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Modify: `frontend/src/api/admin/groups.ts` if existing group list helper needs typing.
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Test: relevant settings view tests if present; otherwise add a focused Vitest under `frontend/src/views/admin/__tests__/SettingsView.imageStudio.spec.ts`

- [x] **Step 1: Write failing frontend settings test**

Create or update a test that mounts the settings view with settings response:

```ts
{
  image_studio_available_group_ids: [10, 12],
  image_generation_tool_declaration_policy: 'strip'
}
```

Assert the image studio tab renders:

- available group selector
- declaration policy selector
- selected policy `strip`

Run:

```bash
cd frontend && pnpm vitest run src/views/admin/__tests__/SettingsView.imageStudio.spec.ts
```

Expected now: FAIL because controls do not exist.

- [x] **Step 2: Add TypeScript settings fields**

In `frontend/src/api/admin/settings.ts`, add to `SystemSettings` and update request type:

```ts
image_studio_available_group_ids: number[];
image_generation_tool_declaration_policy: 'strip' | 'allow' | 'reject';
```

Optional exported type:

```ts
export type ImageGenerationToolDeclarationPolicy = 'strip' | 'allow' | 'reject'
```

- [x] **Step 3: Add form defaults and save payload fields**

In `frontend/src/views/admin/SettingsView.vue` form defaults, add:

```ts
image_studio_available_group_ids: [],
image_generation_tool_declaration_policy: 'strip',
```

In save payload, add:

```ts
image_studio_available_group_ids: Array.isArray(form.image_studio_available_group_ids)
  ? [...new Set(form.image_studio_available_group_ids.map(Number).filter((id) => Number.isFinite(id) && id > 0))]
  : [],
image_generation_tool_declaration_policy: ['strip', 'allow', 'reject'].includes(form.image_generation_tool_declaration_policy)
  ? form.image_generation_tool_declaration_policy
  : 'strip',
```

- [x] **Step 4: Add controls to Image Studio tab**

In the existing Image Studio tab card, add:

```vue
<div>
  <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
    {{ t("admin.settings.imageStudio.availableGroups") }}
  </label>
  <!-- Use the existing project multi-select/select component if SettingsView already uses one. -->
  <select v-model="form.image_studio_available_group_ids" multiple class="input min-h-32">
    <option v-for="group in groups" :key="group.id" :value="group.id">
      {{ group.name }} · {{ group.platform }} · {{ group.allow_image_generation ? t("admin.settings.imageStudio.groupImageEnabled") : t("admin.settings.imageStudio.groupImageDisabled") }}
    </option>
  </select>
  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
    {{ t("admin.settings.imageStudio.availableGroupsHint") }}
  </p>
</div>

<div>
  <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
    {{ t("admin.settings.imageStudio.toolDeclarationPolicy") }}
  </label>
  <select v-model="form.image_generation_tool_declaration_policy" class="input">
    <option value="strip">{{ t("admin.settings.imageStudio.toolDeclarationPolicyStrip") }}</option>
    <option value="allow">{{ t("admin.settings.imageStudio.toolDeclarationPolicyAllow") }}</option>
    <option value="reject">{{ t("admin.settings.imageStudio.toolDeclarationPolicyReject") }}</option>
  </select>
  <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
    {{ t("admin.settings.imageStudio.toolDeclarationPolicyHint") }}
  </p>
</div>
```

If `SettingsView.vue` already uses a custom multi-select component, use that instead of native `<select multiple>`.

- [x] **Step 5: Add i18n labels**

In `frontend/src/i18n/locales/zh.ts`:

```ts
availableGroups: '生图体验可用分组',
availableGroupsHint: '只有选中分组下符合条件的 API Key 才会出现在生图体验中。',
groupImageEnabled: '已开启生图',
groupImageDisabled: '未开启生图',
toolDeclarationPolicy: '客户端生图工具声明策略',
toolDeclarationPolicyHint: '控制 Responses 客户端预声明 image_generation 工具时的处理方式。',
toolDeclarationPolicyStrip: '剥离声明并继续请求（推荐）',
toolDeclarationPolicyAllow: '允许声明，实际生图仍受分组开关限制',
toolDeclarationPolicyReject: '拒绝带声明的请求',
```

Add equivalent English text in `frontend/src/i18n/locales/en.ts`.

- [x] **Step 6: Run tests and commit**

Run:

```bash
cd frontend && pnpm vitest run src/views/admin/__tests__/SettingsView.imageStudio.spec.ts
cd frontend && pnpm type-check
```

Expected: PASS.

Commit:

```bash
git add frontend/src/api/admin/settings.ts frontend/src/views/admin/SettingsView.vue frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts frontend/src/views/admin/__tests__/SettingsView.imageStudio.spec.ts
git commit -m "feat: add image studio admin controls UI"
```

---

### Task 5: Image studio frontend key filtering

**Files:**
- Modify: `frontend/src/extensions/image-studio/ImageStudioView.vue`
- Modify: `frontend/src/extensions/image-studio/components/GenerationSettingsPanel.vue` if empty copy is passed there.
- Test: `frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts`

- [x] **Step 1: Write failing key filtering test**

In `ImageStudioView.spec.ts`, mock settings so:

```ts
image_studio_available_group_ids: [100]
```

Mock user keys:

- key A: active, OpenAI, allow image generation, group id 100
- key B: active, OpenAI, allow image generation, group id 101
- key C: active, OpenAI, image generation disabled, group id 100

Expected selectable options: only key A.

Run:

```bash
cd frontend && pnpm vitest run src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
```

Expected now: FAIL because group allowlist is not used.

- [x] **Step 2: Fetch settings for image studio page**

In `ImageStudioView.vue`, import the admin/public settings API that is available to authenticated users. If admin settings are not accessible to normal users, add a safe backend/public field in Task 1 before implementing this step. The component needs only:

```ts
const imageStudioAvailableGroupIds = ref<number[]>([])
```

Load it before or alongside API keys:

```ts
const settings = await adminAPI.settings.getSettings()
imageStudioAvailableGroupIds.value = Array.isArray(settings.image_studio_available_group_ids)
  ? settings.image_studio_available_group_ids
  : []
```

If normal users cannot call admin settings, use a public/user-safe endpoint and update tests accordingly.

- [x] **Step 3: Update key predicate**

Replace `isImageStudioApiKey` with:

```ts
function isImageStudioApiKey(key: StudioApiKey) {
  const groupId = Number(key.group?.id)
  return key.status === 'active' &&
    key.group?.platform === 'openai' &&
    key.group?.allow_image_generation === true &&
    Number.isFinite(groupId) &&
    imageStudioAvailableGroupIds.value.includes(groupId)
}
```

Ensure the initial selected key is cleared if no key remains.

- [x] **Step 4: Update empty state copy**

Change empty text from generic “create an API key” to mention administrator-enabled groups:

```text
没有可用于生图体验的 API 密钥。请确认管理员已在生图设置中选择可用分组，且该分组已开启生图。
```

- [x] **Step 5: Run tests and commit**

Run:

```bash
cd frontend && pnpm vitest run src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
cd frontend && pnpm type-check
```

Expected: PASS.

Commit:

```bash
git add frontend/src/extensions/image-studio/ImageStudioView.vue frontend/src/extensions/image-studio/components/GenerationSettingsPanel.vue frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
git commit -m "feat: filter image studio keys by admin group allowlist"
```

---

### Task 6: Final verification and OpenSpec task sync

**Files:**
- Modify: `openspec/changes/configure-image-studio-available-groups/tasks.md`
- Modify: this plan file as tasks are completed.

- [x] **Step 1: Run backend focused tests**

```bash
cd backend && go test ./internal/service ./internal/handler ./internal/handler/admin -run 'ImageStudio|ImageGeneration|DeclarationPolicy|Settings' -count=1
```

Expected: PASS.

- [x] **Step 2: Run frontend focused tests**

```bash
cd frontend && pnpm vitest run src/extensions/image-studio/__tests__/ImageStudioView.spec.ts src/views/admin/__tests__/SettingsView.imageStudio.spec.ts
```

Expected: PASS.

- [x] **Step 3: Run type/build checks**

```bash
cd frontend && pnpm type-check
cd backend && go test ./...
```

Expected: PASS. If `go test ./...` is too slow for local iteration, run the focused packages first and record the full command result before verify.

- [x] **Step 4: Sync OpenSpec tasks**

Mark completed tasks in `openspec/changes/configure-image-studio-available-groups/tasks.md`:

```markdown
- [x] 1.1 Add an image studio available group IDs setting key, default, parser, normalizer, and service field.
```

Continue for all completed task lines.

- [x] **Step 5: Commit task sync**

```bash
git add openspec/changes/configure-image-studio-available-groups/tasks.md docs/superpowers/plans/2026-07-09-image-studio-admin-controls.md
git commit -m "chore: sync image studio admin controls tasks"
```

---

### Task 7: Restore usage-card billing after the service split

**Files:**
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Test: `backend/internal/service/gateway_service_subscription_billing_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`

- [ ] **Step 1: Run the existing usage-card command test and confirm RED**
- [ ] **Step 2: Add focused tests for billing priority and both production recorder call sites**
- [ ] **Step 3: Restore `BillingPriority`, usage-card enablement, group override, cost, and ID propagation from pre-merge commit `58583dbd`**
- [ ] **Step 4: Run focused billing tests and commit**

---

### Task 8: Restore lossless admin settings round trips

**Files:**
- Modify: `backend/internal/handler/admin/setting_handler.go`
- Modify: `backend/internal/handler/admin/setting_handler_update.go`
- Modify: `backend/internal/handler/admin/setting_handler_audit.go`
- Modify: `backend/internal/service/setting_update.go`
- Modify: `backend/internal/service/setting_usage_card_long_context.go`
- Test: admin settings handler and setting service tests

- [ ] **Step 1: Add a failing unrelated-save preservation test**
- [ ] **Step 2: Add failing GET/update response tests for legacy subscriptions, usage cards, default usage cards, and long-context billing**
- [ ] **Step 3: Port request fields, previous-value merging, validation, DTO conversion, persistence, response, and audit behavior from `58583dbd`**
- [ ] **Step 4: Restore default usage-card parsing compatibility and run focused tests**
- [ ] **Step 5: Commit the settings reconciliation**

---

### Task 9: Reconcile image intent and passthrough policy

**Files:**
- Modify: `backend/internal/service/image_generation_intent.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_passthrough.go`
- Modify: `backend/internal/service/openai_ws_forwarder_ingress.go`
- Modify: declaration and gateway tests

- [ ] **Step 1: Add failing namespace group-gate tests for raw/map and HTTP/WS paths**
- [ ] **Step 2: Add a failing passthrough `strip`/`allow`/`reject` matrix**
- [ ] **Step 3: Limit passive classification to native flat declarations and retain namespace/additional-tools as actual image intent**
- [ ] **Step 4: Apply shared declaration/access preflight before passthrough and run focused tests**
- [ ] **Step 5: Commit the gateway reconciliation**

---

### Task 10: Restore fallback image-protocol scheduling

**Files:**
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Test: OpenAI scheduler tests

- [ ] **Step 1: Add a failing test for `responses` preference with the scheduler unavailable**
- [ ] **Step 2: Propagate protocol preference through model selection, load-aware sorting, and fallback wait ordering**
- [ ] **Step 3: Run scheduler tests and commit**

---

### Task 11: Re-verify the merged implementation

- [ ] **Step 1: Run focused service, handler, and frontend tests**
- [ ] **Step 2: Run the complete backend unit suite and frontend typecheck/tests**
- [ ] **Step 3: Run a post-merge code review and resolve critical findings**
- [ ] **Step 4: Sync OpenSpec and plan task checkboxes, then commit verification evidence**
