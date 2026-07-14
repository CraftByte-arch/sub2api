---
change: refactor-image-studio-edit-storage
design-doc: docs/superpowers/specs/2026-07-14-image-studio-edit-storage-design.md
base-ref: e15265205a50addfeba66f935b7e256ea2a51f20
---

# Image Studio 编辑输入文件存储实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Image Studio 异步编辑任务改为上传 1 到 4 张压缩参考图及可选蒙版，后端仅持久化受控相对路径，并在 Worker 执行时重建协议正确的上游请求和完整输入生命周期。

**Architecture:** 浏览器对参考图执行同尺寸 WebP 0.72 压缩并以 multipart 上传，蒙版保持原始二进制；后端文件优先落入共享 `DATA_DIR/image-studio/inputs`，数据库只保存有序相对路径和过期元数据。Worker 根据账号类型构建 API Key multipart 磁盘 spool 或 OAuth Responses 临时 data URL，并通过原子领取、幂等删除、孤儿扫描和存储探针控制生命周期。

**Tech Stack:** Vue 3、TypeScript、Canvas API、Vitest、Go、Gin、PostgreSQL、`mime/multipart`、sqlmock、testify、Wire。

---

## 文件职责

- `backend/migrations/175_image_studio_edit_input_storage.sql`：新增输入路径/TTL 字段，清理终态遗留 payload。
- `backend/internal/service/image_studio_input_store.go`：根目录约束、校验、原子落盘、读取、删除、probe、遗留转存和孤儿扫描。
- `backend/internal/service/image_studio_edit_multipart.go`：从磁盘输入构建短生命周期 multipart spool。
- `backend/internal/repository/image_studio_job_repo.go`：路径元数据、领取/过期互斥、遗留原子更新和清理查询。
- `backend/internal/handler/image_studio_job_handler.go`：JSON generation 与 multipart edit 分派。
- `backend/internal/service/image_studio_job_service.go`：文件优先创建、DB 失败回滚、TTL、删除和 cleanup。
- `backend/internal/service/image_studio_job_worker.go`：加载输入、协议分派、错误分类、成功后清理。
- `frontend/src/extensions/image-studio/imageCompression.ts`：同尺寸 WebP 0.72 转换。
- `frontend/src/extensions/image-studio/imageStudioApi.ts` 与 `ImageStudioView.vue`：编辑上传 File，生成保持 JSON。

### Task 1: 数据库迁移与输入元数据模型（OpenSpec 1.1、1.2）

**Files:**
- Create: `backend/migrations/175_image_studio_edit_input_storage.sql`
- Create: `backend/migrations/image_studio_edit_input_storage_test.go`
- Modify: `backend/internal/repository/migrations_schema_integration_test.go`
- Modify: `backend/internal/service/image_studio_job.go`
- Modify: `backend/internal/repository/image_studio_job_repo.go`
- Modify: `backend/internal/repository/image_studio_job_repo_test.go`
- Modify: Image Studio repository stubs under `backend/internal/service/*_test.go` and `backend/internal/handler/image_studio_job_handler_test.go`

- [x] **Step 1: 写 RED 测试**

增加测试验证四个新列、`input_image_paths` 顺序、终态 edit payload 删除 `images`/`mask`、活动遗留 payload 保留，以及响应 DTO 不暴露路径。

运行：

```bash
cd backend
go test ./migrations ./internal/repository ./internal/handler -run 'ImageStudio.*(Migration|InputPath|Response)' -count=1
```

预期：FAIL，列和模型字段尚不存在。

- [x] **Step 2: 实现迁移**

迁移增加：

```sql
input_image_paths JSONB NOT NULL DEFAULT '[]'::jsonb
input_mask_path TEXT NULL
input_expires_at TIMESTAMPTZ NULL
input_deleted_at TIMESTAMPTZ NULL
```

对 `mode='edit'` 且 `status IN ('succeeded','failed')` 的 row 执行 `request_payload - 'images' - 'mask'`，并创建 `(input_expires_at, input_deleted_at, status, id)` 清理索引。

- [x] **Step 3: 扩展模型和稳定扫描**

给 `ImageStudioJob`/`ImageStudioJobCreateInput` 增加 `InputImagePaths []string`、`InputMaskPath *string`、`InputExpiresAt *time.Time`、`InputDeletedAt *time.Time`。仓库扫描 JSONB 后拒绝非字符串数组和超过四项，Create 写入路径和 TTL。

- [x] **Step 4: GREEN 并提交**

```bash
cd backend
go test ./migrations ./internal/repository ./internal/handler ./internal/service -run ImageStudio -count=1
git add migrations/175_image_studio_edit_input_storage.sql migrations/image_studio_edit_input_storage_test.go internal/repository/migrations_schema_integration_test.go internal/service/image_studio_job.go internal/repository/image_studio_job_repo.go internal/repository/image_studio_job_repo_test.go internal/service/*image_studio*_test.go internal/handler/image_studio_job_handler_test.go
git commit -m "feat: add image studio edit input metadata"
```

### Task 2: 根目录约束输入存储（OpenSpec 1.3）

**Files:**
- Create: `backend/internal/service/image_studio_input_store.go`
- Create: `backend/internal/service/image_studio_input_store_test.go`

- [ ] **Step 1: 写 RED 表格测试**

覆盖 1/4/5 张图、每文件超限、伪 MIME、不可解码、蒙版无有效 alpha、蒙版尺寸不匹配、绝对路径、`..`、symlink escape、原子 finalize、失败回滚和幂等删除。

```bash
cd backend
go test ./internal/service -run ImageStudioInputStore -count=1
```

预期：FAIL，存储类型不存在。

- [ ] **Step 2: 定义明确接口**

实现 `UploadedFile`、`StagedEditInputs`、`OpenedEditInputs`，以及：

```go
StageEditInputs(ctx context.Context, images []UploadedFile, mask *UploadedFile) (*StagedEditInputs, error)
OpenInputs(paths []string, maskPath *string) (*OpenedEditInputs, error)
RemoveInputs(paths []string, maskPath *string) error
MaterializeLegacy(ctx context.Context, images []string, mask *string) (*StagedEditInputs, error)
Probe() error
CleanupOrphans(ctx context.Context, referenced map[string]struct{}, now time.Time) error
```

- [ ] **Step 3: 实现校验与落盘**

根为 `DATA_DIR/image-studio`。随机私有目录内先写 `.<name>.tmp`，用 bounded reader、`http.DetectContentType`、`image.DecodeConfig` 和 `image.Decode` 校验，再 `os.Rename` 为服务器命名。引用图最多 4 张；mask 必须透明格式、有可用 alpha、尺寸等于第一张图。任何失败 `RemoveAll(uploadDir)`。

所有 open/remove 先拒绝绝对路径和 `..`，再解析 symlink 并用 `filepath.Rel` 保证仍在 root 下。

- [ ] **Step 4: GREEN、race test 和提交**

```bash
cd backend
go test ./internal/service -run ImageStudioInputStore -count=1
go test -race ./internal/service -run ImageStudioInputStoreStages -count=1
git add internal/service/image_studio_input_store.go internal/service/image_studio_input_store_test.go
git commit -m "feat: add image studio input store"
```

### Task 3: 仓库原子领取、过期和遗留更新（OpenSpec 1.4、4.2）

**Files:**
- Modify: `backend/internal/service/image_studio_job.go`
- Modify: `backend/internal/repository/image_studio_job_repo.go`
- Modify: `backend/internal/repository/image_studio_job_repo_test.go`
- Modify: repository stubs in Image Studio tests

- [ ] **Step 1: 写 RED SQL 行为测试**

覆盖 `MarkRunning` 仅领取未过期 queued row、`ExpireQueuedInputs` 使用 `FOR UPDATE SKIP LOCKED` 原子改为 `failed/input_expired`、`ListExpiredInputs` 排除 running、`PersistLegacyInputs` 同一 UPDATE 写路径并 redaction、`MarkInputsDeleted` 幂等和引用目录列表。

- [ ] **Step 2: 增加仓库接口**

```go
PersistLegacyInputs(ctx context.Context, id int64, paths []string, maskPath *string, redacted json.RawMessage, expiresAt time.Time) error
ExpireQueuedInputs(ctx context.Context, now time.Time, limit int) ([]ImageStudioJob, error)
ListExpiredInputs(ctx context.Context, now time.Time, limit int) ([]ImageStudioJob, error)
MarkInputsDeleted(ctx context.Context, id int64, deletedAt time.Time) error
ListReferencedInputDirs(ctx context.Context) (map[string]struct{}, error)
```

`MarkRunning` WHERE 增加 `input_expires_at IS NULL OR input_expires_at > now`，确保 claim 与 expiry 互斥。

- [ ] **Step 3: GREEN 并提交**

```bash
cd backend
go test ./internal/repository ./internal/service ./internal/handler -run 'ImageStudio.*(Legacy|Expired|Input)' -count=1
git add internal/service/image_studio_job.go internal/repository/image_studio_job_repo.go internal/repository/image_studio_job_repo_test.go internal/service/*image_studio*_test.go internal/handler/image_studio_job_handler_test.go
git commit -m "feat: add image studio input lifecycle queries"
```

### Task 4: 独立输入 TTL 管理设置（OpenSpec 4.2）

**Files:**
- Modify: `backend/internal/service/domain_constants.go`
- Modify: `backend/internal/service/settings_view.go`
- Modify: `backend/internal/service/setting_parse.go`
- Modify: `backend/internal/service/setting_update.go`
- Modify: `backend/internal/handler/dto/settings.go`
- Modify: `backend/internal/handler/admin/setting_handler.go`
- Modify: `backend/internal/handler/admin/setting_handler_update.go`
- Modify: `frontend/src/api/admin/settings.ts`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/en/admin/settings.ts`
- Test: `backend/internal/service/setting_service_update_test.go`
- Test: `backend/internal/handler/admin/setting_handler_platform_quota_test.go`
- Test: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

- [ ] **Step 1: 写 RED 默认值/API/UI 测试**

断言缺失、0、负数均读取为 24，合法正整数保存；admin GET/PUT 和表单使用 `image_studio_input_retention_hours`，不改变输出 retention。

- [ ] **Step 2: 实现后端设置链**

新增 `SettingKeyImageStudioInputRetentionHours` 和默认 24；解析/更新将非正值归一为 24，DTO 和 admin handler 完整透传。

- [ ] **Step 3: 实现管理员输入**

在 Image Studio 设置区增加 `min=1 step=1` 数字输入；提交值为 `Math.max(1, Math.floor(Number(value) || 24))`，补齐中英文文案。

- [ ] **Step 4: GREEN 并提交**

```bash
cd backend && go test ./internal/service ./internal/handler/admin -run 'ImageStudio|Setting' -count=1
cd ../frontend && pnpm exec vitest run src/views/admin/__tests__/SettingsView.spec.ts
git add ../backend/internal/service/domain_constants.go ../backend/internal/service/settings_view.go ../backend/internal/service/setting_parse.go ../backend/internal/service/setting_update.go ../backend/internal/service/setting_service_update_test.go ../backend/internal/handler/dto/settings.go ../backend/internal/handler/admin/setting_handler.go ../backend/internal/handler/admin/setting_handler_update.go ../backend/internal/handler/admin/setting_handler_platform_quota_test.go src/api/admin/settings.ts src/views/admin/SettingsView.vue src/views/admin/__tests__/SettingsView.spec.ts src/i18n/locales/zh/admin/settings.ts src/i18n/locales/en/admin/settings.ts
git commit -m "feat: configure image studio input retention"
```

### Task 5: multipart 编辑任务创建与 JSON 兼容边界（OpenSpec 2.1、2.2）

**Files:**
- Modify: `backend/internal/handler/image_studio_job_handler.go`
- Modify: `backend/internal/handler/image_studio_job_handler_test.go`
- Modify: `backend/internal/service/image_studio_job_service.go`
- Modify: `backend/internal/service/image_studio_job_service_test.go`

- [ ] **Step 1: 写 RED 创建测试**

覆盖重复有序 `image` 的 1/4 张、零图、第五张、第二个 mask、文件校验失败不建 row、repo Create 失败删目录、JSON generation 不变、JSON edit data URL 返回兼容错误、storage unavailable 返回 503。

- [ ] **Step 2: 分派 Content-Type 并净化 payload**

`application/json` 只走 generation；`multipart/form-data` 只走 edit。使用 `MultipartReader` 顺序流式读取 part，不用 `ParseMultipartForm`。edit 的 `request_payload` 只保留模型、prompt、size、quality、background、style、moderation、input_fidelity、output_format/compression 和 response_format，禁止 `images`、`mask`、data URL/base64。

- [ ] **Step 3: 文件优先创建和回滚**

新增 `CreateEditJob`：先 `StageEditInputs`，计算 `InputExpiresAt = now + inputRetentionHours`，再 `repo.Create`；DB 失败调用 `RemoveInputs`。generation 继续调用现有 `CreateJob`。

- [ ] **Step 4: GREEN 并提交**

```bash
cd backend
go test ./internal/handler ./internal/service ./internal/repository -run 'ImageStudio.*Create' -count=1
git add internal/handler/image_studio_job_handler.go internal/handler/image_studio_job_handler_test.go internal/service/image_studio_job_service.go internal/service/image_studio_job_service_test.go
git commit -m "feat: accept multipart image studio edits"
```

### Task 6: 前端同尺寸压缩与四图上传（OpenSpec 2.3、2.4）

**Files:**
- Create: `frontend/src/extensions/image-studio/imageCompression.ts`
- Create: `frontend/src/extensions/image-studio/__tests__/imageCompression.spec.ts`
- Modify: `frontend/src/extensions/image-studio/imageStudioApi.ts`
- Modify: `frontend/src/extensions/image-studio/__tests__/imageStudioApi.spec.ts`
- Modify: `frontend/src/extensions/image-studio/ImageStudioView.vue`
- Modify: `frontend/src/extensions/image-studio/__tests__/ImageStudioView.spec.ts`

- [ ] **Step 1: 写 RED 压缩/API/UI 测试**

mock `createImageBitmap` 和 canvas，断言宽高不变、`toBlob('image/webp', 0.72)`、确定性文件名。断言 FormData 中四个 `image` 顺序不变、mask 与原 File 同一对象、未手工设置 Content-Type。任一压缩失败时 API 调用次数为 0。

- [ ] **Step 2: 实现 `compressReferenceImage(file, index)`**

解码原图，canvas 设为 bitmap 原宽高，绘制后输出 WebP 0.72；`toBlob` 返回 null 或解码失败时 reject，并确保 bitmap 关闭。

- [ ] **Step 3: 改造 API 和 View**

`ImageStudioJobCreateInput` 使用 `imageFiles?: File[]`、`maskFile?: File`。generation 保持 JSON；edit 创建 FormData、按显示顺序重复 append `image`，可选 append `mask`。View 在网络调用前用 `Promise.all` 压缩全部 1 到 4 张参考图，mask 不经过 canvas。

- [ ] **Step 4: GREEN、typecheck 和提交**

```bash
cd frontend
pnpm exec vitest run src/extensions/image-studio/__tests__/imageCompression.spec.ts src/extensions/image-studio/__tests__/imageStudioApi.spec.ts src/extensions/image-studio/__tests__/ImageStudioView.spec.ts
pnpm run typecheck
git add src/extensions/image-studio
git commit -m "feat: upload compressed image studio references"
```

### Task 7: 活动遗留 data URL 转存（OpenSpec 1.4）

**Files:**
- Modify: `backend/internal/service/image_studio_input_store.go`
- Modify: `backend/internal/service/image_studio_input_store_test.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Modify: `backend/internal/service/image_studio_job_worker_test.go`

- [ ] **Step 1: 写 RED 遗留测试**

覆盖 1/4 张有序 data URL、可选 mask、零/五张、坏 base64、repo 更新失败删除新目录，以及成功时路径写入与 payload redaction 同时发生。

- [ ] **Step 2: 实现 `MaterializeLegacy`**

只接受受支持的 image data URL，使用 bounded base64 decoder 并复用 Task 2 的内容/mask 校验。不得记录 bytes 或完整 data URL。

- [ ] **Step 3: Worker 首次领取后原子替换**

edit job 无路径但 payload 含 `images` 时 materialize，构建去掉 `images`/`mask` 的 JSON，再调用 `PersistLegacyInputs`。更新失败删除目录；无效遗留输入标记 `legacy_input_invalid` 且清除可安全 redaction 的二进制字段，避免重复解码。

- [ ] **Step 4: GREEN 并提交**

```bash
cd backend
go test ./internal/service ./internal/repository -run 'ImageStudio.*Legacy' -count=1
git add internal/service/image_studio_input_store.go internal/service/image_studio_input_store_test.go internal/service/image_studio_job_worker.go internal/service/image_studio_job_worker_test.go
git commit -m "feat: materialize legacy image studio edits"
```

### Task 8: Worker 输入验证与终态存储错误（OpenSpec 3.1、3.4）

**Files:**
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Modify: `backend/internal/service/image_studio_job_worker_test.go`

- [ ] **Step 1: 写 RED 分类测试**

注入 recording upstream，分别制造 expired、missing、corrupt、unsafe path；断言 upstream 调用为 0，错误码为 `input_expired`、`input_missing`、`input_invalid`、`input_path_invalid`，且不进入 retry。

- [ ] **Step 2: 在上游前加载文件**

edit job 在成功 `MarkRunning` 后、账号选择/调用上游前比较 TTL 并 `OpenInputs`，defer 关闭句柄；generation 不经过 store。

- [ ] **Step 3: 保持 provider retry 语义**

稳定输入错误直接 `MarkFailed`。429、retryable 5xx 和 transport error 继续走现有 `MarkRetryable`，且 TTL 前不删除输入。

- [ ] **Step 4: GREEN 并提交**

```bash
cd backend
go test ./internal/service -run 'ImageStudio.*(Input|Storage|Retry)' -count=1
git add internal/service/image_studio_job_worker.go internal/service/image_studio_job_worker_test.go
git commit -m "feat: validate image studio inputs before forwarding"
```

### Task 9: API Key multipart 磁盘 spool（OpenSpec 3.2、3.4）

**Files:**
- Create: `backend/internal/service/image_studio_edit_multipart.go`
- Create: `backend/internal/service/image_studio_edit_multipart_test.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/openai_images_test.go`
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Modify: `backend/internal/service/image_studio_job_worker_test.go`

- [ ] **Step 1: 写 RED multipart 测试**

解析 spool，断言 1/4 个 repeated `image` 的顺序、至多一个 mask、全部 edit 标量、正确 boundary 和 Content-Length。分别模拟成功、上游失败、取消、响应解析失败，断言 spool 均已删除。

- [ ] **Step 2: 实现 spool builder**

在受控 upload directory 创建 `.spool-<random>.multipart`，用 `multipart.NewWriter(*os.File)` 和 `io.Copy` 写入；返回 path/content-type/content-length。任意构建错误立即删除。

- [ ] **Step 3: 增加 gateway 流式 API Key edit 入口**

接收 `io.ReadSeeker`、content type 和 content length，直接构造上游 request，不把 multipart 重写进 `[]byte`。映射后的 model 由 builder 写入；复用现有 API Key images 响应、failover、ops 和 billing 处理。

- [ ] **Step 4: Worker 分派并 defer 清理**

仅 `AccountTypeAPIKey` 构建 spool，创建成功后立即 `defer os.Remove(path)`，endpoint 固定 `/v1/images/edits`。

- [ ] **Step 5: GREEN 并提交**

```bash
cd backend
go test ./internal/service -run 'ImageStudio.*Multipart|OpenAIImages.*APIKeyEdit' -count=1
git add internal/service/image_studio_edit_multipart.go internal/service/image_studio_edit_multipart_test.go internal/service/openai_images.go internal/service/openai_images_test.go internal/service/image_studio_job_worker.go internal/service/image_studio_job_worker_test.go
git commit -m "feat: stream image studio edit multipart"
```

### Task 10: OAuth Responses 转换与计费回归（OpenSpec 3.3、3.4）

**Files:**
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Modify: `backend/internal/service/image_studio_job_worker_test.go`
- Modify: `backend/internal/service/openai_images_responses.go`
- Modify: `backend/internal/service/openai_images_test.go`

- [ ] **Step 1: 写 RED OAuth 测试**

从四个磁盘输入捕获 Responses JSON，断言 `input_image` 顺序和 mask；断言 job payload/仓库写入不含 `data:` 或 base64，并保持现有 result decoding、subscription 和 billing metadata。

- [ ] **Step 2: 构造请求期临时 data URL**

读取已打开文件时执行总输入大小上限，构造局部 `OpenAIImagesUpload` 并复用 `buildOpenAIImagesResponsesRequest`。临时 bytes/data URL 不回写 job，不进入日志。

- [ ] **Step 3: 保持路由字段**

继续使用 Responses image-protocol account selection；settlement 中 inbound endpoint 为 `/v1/images/edits`，upstream endpoint 为 `/v1/responses`。

- [ ] **Step 4: GREEN 并提交**

```bash
cd backend
go test ./internal/service -run 'ImageStudio.*OAuth|OpenAIImages.*OAuth|ImageStudio.*(Settlement|Billing)' -count=1
git add internal/service/image_studio_job_worker.go internal/service/image_studio_job_worker_test.go internal/service/openai_images_responses.go internal/service/openai_images_test.go
git commit -m "feat: execute stored image edits through oauth"
```

### Task 11: 成功、TTL、删除和孤儿清理（OpenSpec 4.1、4.2、4.3、4.4）

**Files:**
- Modify: `backend/internal/service/image_studio_job_worker.go`
- Modify: `backend/internal/service/image_studio_job_worker_test.go`
- Modify: `backend/internal/service/image_studio_job_service.go`
- Modify: `backend/internal/service/image_studio_job_service_test.go`
- Modify: `backend/internal/service/image_studio_input_store.go`
- Modify: `backend/internal/service/image_studio_input_store_test.go`
- Modify: `backend/internal/repository/image_studio_job_repo.go`
- Modify: `backend/internal/repository/image_studio_job_repo_test.go`

- [ ] **Step 1: 写 RED 生命周期测试**

覆盖：`MarkSettling` durable 后删输入；settlement retry 无需输入；provider retryable 保留；queued TTL 原子失败并删除；running 即使过 TTL 也跳过 cleanup；用户删除顺序 input/output/row；referenced/young orphan 保留、old orphan/stale spool 删除；单项失败不中断批次。

- [ ] **Step 2: 放置成功删除点**

只有输出文件与 settlement recovery payload 已由 `MarkSettling` 持久化后才调用 `RemoveInputs` 和 `MarkInputsDeleted`。删除或 timestamp 更新失败不得回滚成功结果，下轮 cleanup 幂等重试。

- [ ] **Step 3: 扩展 cleanup loop**

顺序为：`ExpireQueuedInputs(now, 50)`、删除过期非 running 输入、现有 output cleanup、读取 DB 引用后孤儿目录扫描、stale spool 清理。DB 引用查询失败时跳过孤儿删除。

- [ ] **Step 4: 扩展用户删除**

`DeleteJob` 先删除 input，再删除 original/thumbnail，最后删 row；缺失文件视为已删除，真实 I/O 失败保留 row 便于重试。

- [ ] **Step 5: GREEN 并提交**

```bash
cd backend
go test ./internal/service ./internal/repository -run 'ImageStudio.*(Lifecycle|Expired|Delete|Orphan|Spool|Cleanup|Retry)' -count=1
git add internal/service/image_studio_job_worker.go internal/service/image_studio_job_worker_test.go internal/service/image_studio_job_service.go internal/service/image_studio_job_service_test.go internal/service/image_studio_input_store.go internal/service/image_studio_input_store_test.go internal/repository/image_studio_job_repo.go internal/repository/image_studio_job_repo_test.go
git commit -m "feat: manage image studio input lifecycle"
```

### Task 12: 存储探针、Worker 暂停与依赖注入（OpenSpec 4.4、5.4）

**Files:**
- Modify: `backend/internal/service/image_studio_input_store.go`
- Modify: `backend/internal/service/image_studio_input_store_test.go`
- Modify: `backend/internal/service/image_studio_job_service.go`
- Modify: `backend/internal/service/image_studio_job_service_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`
- Test: `backend/cmd/server/wire_gen_test.go`

- [ ] **Step 1: 写 RED probe 测试**

模拟 root 只读/不可用，断言 probe 完成 create/write/read/delete；失败时异步 Image Studio 创建返回 503、Worker 不领取新任务、无关 API 和已有 output download 不受影响；恢复后自动重新领取。

- [ ] **Step 2: 实现周期健康状态**

启动立即 probe，之后周期重试；失败只标记 Image Studio input storage unavailable。queue drain 先检查健康状态，handler 通过稳定 `input_storage_unavailable` 映射 503。

- [ ] **Step 3: Wire 注入共享 store**

`ProvideImageStudioJobService` 创建/接收同一 `DATA_DIR/image-studio` store 并注入 service；更新生成 Wire。所有接收和执行实例必须挂载相同持久化 `DATA_DIR`。

- [ ] **Step 4: GREEN 并提交**

```bash
cd backend
go test ./internal/service ./internal/handler ./cmd/server -run 'ImageStudio.*(Probe|Storage)|Wire' -count=1
git add internal/service/image_studio_input_store.go internal/service/image_studio_input_store_test.go internal/service/image_studio_job_service.go internal/service/image_studio_job_service_test.go internal/service/wire.go cmd/server/wire_gen.go cmd/server/wire_gen_test.go
git commit -m "feat: gate image studio jobs on shared storage"
```

### Task 13: 全量验证、集成核验与运维文档（OpenSpec 5.1、5.2、5.3、5.4）

**Files:**
- Create: `docs/operations/image-studio-edit-input-storage.md`
- Modify: `openspec/changes/refactor-image-studio-edit-storage/tasks.md`

- [ ] **Step 1: 后端聚焦与全量验证**

```bash
cd backend
go test ./migrations ./internal/repository ./internal/handler ./internal/service -run 'ImageStudio|OpenAIImages' -count=1
go test ./... -count=1
golangci-lint run ./...
```

预期：全部 PASS，无 lint error。

- [ ] **Step 2: 前端验证**

```bash
cd frontend
pnpm exec vitest run src/extensions/image-studio/__tests__/imageCompression.spec.ts src/extensions/image-studio/__tests__/imageStudioApi.spec.ts src/extensions/image-studio/__tests__/ImageStudioView.spec.ts src/views/admin/__tests__/SettingsView.spec.ts
pnpm run lint:check
pnpm run typecheck
pnpm run build
```

预期：全部 PASS，包含四参考图 multipart 场景。

- [ ] **Step 3: 集成环境协议与生命周期核验**

创建一图/四图 API Key edit 和 OAuth edit。SQL 断言新 row 的 `request_payload` 不含 `images`、`mask`、`data:image`，且 `jsonb_array_length(input_image_paths)` 为 1 或 4。测试 upstream 断言 API Key 收到 repeated `image` multipart，OAuth 仍走 Responses。确认 success、TTL、user delete 后 input 消失，output 在既有 retention 到期前可下载。

- [ ] **Step 4: 写部署文档**

记录 schema-first -> 双读 legacy 后端和共享 DATA_DIR -> multipart 前端的发布顺序；确认现有 `deploy/docker-compose.yml` 与 `deploy/docker-compose.standalone.yml` 均把持久卷挂载到 `/app/data`；记录 storage probe、legacy materialization、input/orphan/spool cleanup、数据库 payload 大小监控；强制回滚前停止新 edit 并排空 path-only task，默认 roll-forward。

- [ ] **Step 5: 对照证据勾选 20 项 OpenSpec 任务并提交**

仅在相应测试/集成证据存在后，将 1.1 至 5.4 标记为 `[x]`。

```bash
git add docs/operations/image-studio-edit-input-storage.md openspec/changes/refactor-image-studio-edit-storage/tasks.md
git commit -m "docs: document image studio edit storage rollout"
```

## OpenSpec 覆盖索引

| OpenSpec | 实施任务 |
| --- | --- |
| 1.1 | Task 1 |
| 1.2 | Task 1 |
| 1.3 | Task 2 |
| 1.4 | Task 3、7 |
| 2.1 | Task 5 |
| 2.2 | Task 5 |
| 2.3 | Task 6 |
| 2.4 | Task 6 |
| 3.1 | Task 8 |
| 3.2 | Task 9 |
| 3.3 | Task 10 |
| 3.4 | Task 8、9、10 |
| 4.1 | Task 11 |
| 4.2 | Task 3、4、11 |
| 4.3 | Task 11 |
| 4.4 | Task 11、12 |
| 5.1 | Task 13 Step 1 |
| 5.2 | Task 13 Step 2 |
| 5.3 | Task 13 Step 3 |
| 5.4 | Task 12、13 Step 4 |
