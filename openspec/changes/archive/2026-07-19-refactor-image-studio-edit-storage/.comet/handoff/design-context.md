# Comet Design Handoff

- Change: refactor-image-studio-edit-storage
- Phase: design
- Mode: compact
- Context hash: e43461c8cf046ac0d81d948b1bfb998c6facba31552de1125a73a9517e63716f

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/refactor-image-studio-edit-storage/proposal.md

- Source: openspec/changes/refactor-image-studio-edit-storage/proposal.md
- Lines: 1-32
- SHA256: 3782671d8984779cdd6408bb97b18a73aabe2034ab6d54154a93076fdf4fecb9

```md
## Why

Image Studio 的异步编辑任务目前把参考图和蒙版编码成 data URL 后写入 PostgreSQL，并以 JSON 原样转发到 `/v1/images/edits`。这既不符合 API Key 图片编辑上游需要的 multipart 协议，也导致终态任务长期占用数据库空间并保留用户图片内容，因此需要重构输入文件的上传、持久化、执行和清理生命周期。

## What Changes

- **BREAKING**：Image Studio 编辑任务创建改为 multipart 上传，使用 1 到 4 个重复的 `image` 文件字段和最多 1 个 `mask` 文件字段，不再接受新建任务时提交 `image_data_urls` / `mask_data_url`。
- 前端继续将参考图按原尺寸转换为质量 0.72 的 WebP，再直接上传压缩后的二进制；蒙版保持原始透明通道，不做有损压缩。
- 后端将输入文件写入 `DATA_DIR/image-studio` 下的服务端管理目录，数据库只保存文件路径、过期时间和删除状态，不保存 base64 图片内容。
- 异步 Worker 从本地文件读取参考图，为 API Key 上游构造正确的 multipart `/v1/images/edits` 请求，并继续支持 OAuth/Responses 编辑转换。
- 输入文件在上游结果及结算恢复信息可靠持久化后删除；失败和待重试任务保留到输入文件 TTL，过期后删除并阻止继续执行。
- 用户删除任务时清理输入和输出文件；后台清理过期输入、输出和无数据库引用的孤儿目录。
- 清理历史终态任务中的 base64 图片内容，并为仍可执行的 legacy 编辑任务提供一次性落盘兼容路径。

## Capabilities

### New Capabilities

- `image-studio-edit-jobs`: 定义 Image Studio 异步编辑任务的多参考图上传、本地文件持久化、正确上游转发、重试及文件清理生命周期。

### Modified Capabilities

无。

## Impact

- 前端 Image Studio 提交逻辑、API 客户端和相关组件测试。
- 用户侧 `POST /api/v1/image-studio/jobs` 编辑模式的请求格式及服务端上传校验。
- `image_studio_jobs` 数据库 schema、迁移和 repository 接口。
- Image Studio 异步 Worker、OpenAI Images multipart 构造及 OAuth 兼容路径。
- `DATA_DIR/image-studio` 文件布局、定时清理和手动删除行为。
- 部署必须继续为所有处理 Image Studio 任务的实例提供共享且持久的 `DATA_DIR`。
```

## openspec/changes/refactor-image-studio-edit-storage/design.md

- Source: openspec/changes/refactor-image-studio-edit-storage/design.md
- Lines: 1-119
- SHA256: 798cbde6ed9d6721012537fa05d5da7ac8d4aebd6c37e5be22dc975d85d162b4

[TRUNCATED]

```md
## Context

Image Studio 当前用 JSON 创建异步任务。编辑模式下，前端把参考图转换为 data URL，后端将完整图片内容写入 `image_studio_jobs.request_payload`，Worker 再以 `application/json` 调用 `/v1/images/edits`。API Key 上游通常要求 multipart 文件字段，因此这条路径会失败；同时成功、失败和已过期任务都可能长期保留 base64 输入，造成数据库增长和图片隐私风险。

现有部署已经使用持久化 `DATA_DIR` 保存生成结果和缩略图，因此输入文件可以复用同一持久化边界。蓝绿或多实例部署必须让所有 Image Studio Worker 看到同一个 `DATA_DIR`。

本变更保持为单一 change：上传协议、数据库路径、Worker 读取和清理策略共同组成一个输入文件状态机，任一部分独立交付都会产生不可执行任务或无法回收的文件，因此不具备安全的独立验收边界。

## Goals / Non-Goals

**Goals:**

- 支持 1 到 4 张按顺序排列的参考图和最多 1 张蒙版。
- 前端压缩参考图后直接上传二进制，避免 data URL 和 base64 膨胀。
- 让数据库只保存输入文件的受控相对路径和生命周期元数据。
- 为 API Key 上游生成标准 multipart 编辑请求，同时保持 OAuth/Responses 编辑兼容。
- 在成功、过期、手动删除、创建失败和孤儿清理场景中可靠删除输入文件。
- 平滑处理部署前仍在排队的 legacy data URL 任务，并清除历史终态任务中的图片内容。

**Non-Goals:**

- 不引入 S3 或其他对象存储。
- 不支持超过 4 张参考图。
- 不改变公开网关 `/v1/images/edits` 的请求协议。
- 不改变生成结果、缩略图的现有保留和计费规则。
- 不对蒙版做有损压缩或改变其尺寸。

## Decisions

### 1. 编辑任务使用 multipart 创建，文生图保持 JSON

`POST /api/v1/image-studio/jobs` 根据 Content-Type 和 mode 分流：生成任务继续使用 JSON；编辑任务使用 multipart，元数据使用普通表单字段，参考图使用重复的 `image` 文件字段，蒙版使用单个 `mask` 文件字段。

前端对每张参考图保持原始像素尺寸，转换为质量 0.72 的 WebP `File` 后上传。蒙版保持原始二进制和透明通道。前端压缩是带宽优化，后端仍独立执行数量、大小、MIME 和实际图片解码校验。

替代方案是新增独立编辑任务端点，但会复制鉴权、分组、计费预估和任务历史逻辑，因此保留同一资源端点并使用内容协商。

### 2. 输入文件先原子落盘，再创建任务记录

Handler 为每次上传生成不可预测的随机目录 ID，将文件写入 `DATA_DIR/image-studio/inputs/<upload-id>/`。每个文件先写临时文件，完成校验和 `fsync/close` 后原子 rename 为最终文件名。全部文件落盘成功后再创建数据库任务；数据库写入失败时删除整个上传目录。

数据库新增：

- `input_image_paths`：有序 JSONB 字符串数组，保存相对于 Image Studio 根目录的路径。
- `input_mask_path`：可空相对路径。
- `input_expires_at`：输入文件最晚保留时间。
- `input_deleted_at`：输入文件已删除时间。

路径只能由服务端生成。读取和删除时将相对路径解析到固定根目录，并拒绝绝对路径、`..` 和越界路径。

替代方案是在插入任务后按 job ID 落盘，但数据库提交与文件写入之间的崩溃窗口会产生不可执行的 queued 记录；随机上传目录能让失败回滚和孤儿扫描更直接。

### 3. request_payload 只保存可重建的非二进制参数

`request_payload` 继续保存模型、提示词、尺寸、质量、输出格式等 Worker 所需参数，但不得包含 `images`、`mask`、data URL 或其他图片字节。任务列表继续使用现有独立元数据列，不读取输入文件内容。

### 4. Worker 按账号协议重建请求

Worker 领取编辑任务后先验证输入未删除、未过期且所有路径可读，再解析图片文件。

- API Key 账号：构造 multipart body，按数据库数组顺序写入 1 到 4 个 `image` 文件 part，并写入可选 `mask` part 和普通参数字段。
- OAuth 账号：将文件转成现有 Responses 图片输入结构，继续使用 `image_generation` 工具的 edit action。

Worker 不把文件内容写回数据库。缺失、越界、损坏或过期输入会产生明确的终态错误码，而不是反复重试上游。

### 5. 输入与输出使用不同生命周期

输入文件用于执行和上游重试；输出文件用于历史查看，二者不能共用同一个删除标记。

- 当上游结果、输出文件和 settlement payload 已可靠持久化，任务不再需要重放上游时，立即删除全部输入文件并设置 `input_deleted_at`。
- 可重试失败保留输入文件；终态失败仍保留到 `input_expires_at`，便于在保留窗口内排障或后续重试策略扩展。
- 超过输入 TTL 的 queued/failed 任务删除输入。queued 任务同时标记为 `input_expired`；running 任务不被清理器中途删除，完成后立即清理。
- 用户删除任务时删除输入目录、输出目录并删除数据库记录。
- 现有输出清理继续由 `expires_at/assets_deleted_at` 控制。

输入 TTL 使用独立的 `image_studio_input_retention_hours` 设置，默认 24 小时；现有输出文件保留设置不变。这样延长结果展示时间不会意外延长失败输入的保留期。

### 6. 清理器同时处理受引用输入和孤儿目录

后台清理每轮处理有限批量：
```

Full source: openspec/changes/refactor-image-studio-edit-storage/design.md

## openspec/changes/refactor-image-studio-edit-storage/tasks.md

- Source: openspec/changes/refactor-image-studio-edit-storage/tasks.md
- Lines: 1-34
- SHA256: 4c60df13caf216c1e671bdb7941a2573a42d8ae069497bcd2cad25e644e9f55a

```md
## 1. Database And Storage Foundation

- [ ] 1.1 Add an image studio input-storage migration with ordered input paths, mask path, input expiration/deletion metadata, terminal legacy payload redaction, and migration regression coverage.
- [ ] 1.2 Extend the ImageStudio job domain model and repository reads/writes for input file metadata without exposing paths in user responses.
- [ ] 1.3 Implement a root-confined input storage service with temporary files, atomic finalize, MIME/content/size validation, rollback, and idempotent directory removal.
- [ ] 1.4 Implement active legacy data URL materialization and payload redaction for one to four references plus an optional mask.

## 2. Multipart Job Creation And Frontend Upload

- [ ] 2.1 Accept multipart edit job creation with repeated ordered `image` fields, an optional `mask`, metadata fields, and full rollback on validation or database failure.
- [ ] 2.2 Keep JSON generation job creation behavior unchanged and reject new edit-job data URL payloads with a clear compatibility error.
- [ ] 2.3 Update Image Studio to support one to four references, compress each reference to same-dimension WebP at quality 0.72, and upload compressed Files through multipart without data URL conversion.
- [ ] 2.4 Preserve the mask binary and transparency, stop submission on any compression failure, and cover frontend cardinality, ordering, compression, and multipart behavior with tests.

## 3. Protocol-Correct Worker Execution

- [ ] 3.1 Load and validate stored input files before execution and classify missing, expired, corrupt, or unsafe paths as terminal storage errors.
- [ ] 3.2 Build API Key `/v1/images/edits` multipart requests with ordered repeated image parts, optional mask, and all supported edit metadata.
- [ ] 3.3 Convert stored files through the existing OAuth Responses edit path without persisting image bytes and preserve existing billing/result decoding behavior.
- [ ] 3.4 Add Worker regression tests for one/four images, ordering, mask handling, API Key multipart, OAuth conversion, retries, and terminal storage failures.

## 4. Input Lifecycle And Cleanup

- [ ] 4.1 Delete input files idempotently after result assets and settlement recovery data are durable, while retaining output history under the existing output retention policy.
- [ ] 4.2 Retain inputs for retryable failures, expire queued/failed inputs at the configured safe TTL, mark queued jobs `input_expired`, and never delete files from an actively running job.
- [ ] 4.3 Extend user task deletion to remove input and output directories before deleting the database record.
- [ ] 4.4 Add bounded orphan-directory scanning and cleanup with root confinement, grace periods, and repository/filesystem failure tests.

## 5. Verification And Operational Readiness

- [ ] 5.1 Run focused backend migration, repository, handler, storage, Worker, billing, and cleanup tests, then run the complete backend test suite and linters required by the repository.
- [ ] 5.2 Run Image Studio frontend unit tests, type checking, and production build, including multipart upload and four-reference scenarios.
- [ ] 5.3 Verify in an integration environment that new edit rows contain no base64, API Key upstream requests are multipart, OAuth edits still succeed, inputs disappear after success/TTL/delete, and outputs remain downloadable until output retention expires.
- [ ] 5.4 Document deployment ordering, shared `DATA_DIR` requirements, legacy cleanup metrics, database growth checks, and roll-forward recovery steps.
```

## openspec/changes/refactor-image-studio-edit-storage/specs/image-studio-edit-jobs/spec.md

- Source: openspec/changes/refactor-image-studio-edit-storage/specs/image-studio-edit-jobs/spec.md
- Lines: 1-141
- SHA256: f5f6be5df56b3842782262c42a57a36283ea7da76c8ad886ce787ff24c51b9bc

[TRUNCATED]

```md
## ADDED Requirements

### Requirement: Multipart edit job upload
The system SHALL create Image Studio edit jobs from multipart requests containing between one and four ordered reference image files and at most one mask file.

#### Scenario: Submit one compressed reference image
- **WHEN** a user submits an edit job with one frontend-compressed WebP `image` file and valid edit metadata
- **THEN** the system accepts the multipart request and creates one queued edit job

#### Scenario: Submit four compressed reference images
- **WHEN** a user submits four `image` file fields in a defined order
- **THEN** the system accepts all four files and preserves their order for task execution

#### Scenario: Reject a fifth reference image
- **WHEN** an edit job request contains more than four `image` file fields
- **THEN** the system rejects the request and removes every file staged for that request

#### Scenario: Reject an edit job without a reference image
- **WHEN** an edit job request contains no valid `image` file
- **THEN** the system rejects the request without creating a queued task

#### Scenario: Preserve mask fidelity
- **WHEN** a user includes a mask file with an edit job
- **THEN** the frontend uploads the mask without lossy compression and the backend stores at most one validated mask file

### Requirement: Frontend reference image compression
The Image Studio frontend SHALL convert each selected reference image to a WebP file at quality 0.72 while preserving its pixel dimensions before uploading the binary file.

#### Scenario: Compress four images before upload
- **WHEN** a user selects four supported reference images and submits an edit job
- **THEN** the frontend uploads four compressed WebP files rather than data URLs or the original uncompressed files

#### Scenario: Compression failure stops submission
- **WHEN** any selected reference image cannot be decoded or converted to WebP
- **THEN** the frontend reports the compression error and does not submit a partial edit job

### Requirement: Server-managed input file persistence
The system SHALL persist edit input files under the configured Image Studio data directory and SHALL store only ordered relative paths and lifecycle metadata in the database.

#### Scenario: Persist files before queueing
- **WHEN** all uploaded files pass validation
- **THEN** the system atomically finalizes the files and creates the queued job with their relative paths

#### Scenario: Database creation fails after staging
- **WHEN** input files were staged but the job record cannot be created
- **THEN** the system removes the entire staged input directory

#### Scenario: Database contains no image bytes
- **WHEN** an edit job is created through the new upload flow
- **THEN** its request payload contains no data URL, base64 image, `images`, or `mask` binary content

#### Scenario: Reject unsafe paths
- **WHEN** a stored or derived input path is absolute, traverses with `..`, or resolves outside the Image Studio root
- **THEN** the system refuses to read or delete that path and fails the task with a storage validation error

### Requirement: Edit input validation
The system MUST validate each reference image and mask independently by file count, bounded size, detected MIME type, and decodable image content.

#### Scenario: Reject oversized input
- **WHEN** any uploaded reference image or mask exceeds the configured per-file limit
- **THEN** the system rejects the full request and removes all staged files

#### Scenario: Reject spoofed image MIME
- **WHEN** an uploaded file declares an image MIME type but its content is not a supported decodable image
- **THEN** the system rejects the full request

#### Scenario: Reject an incompatible mask
- **WHEN** an uploaded mask does not preserve supported transparent image content or its pixel dimensions differ from the first reference image
- **THEN** the system rejects the full request and removes all staged files

### Requirement: Input storage availability
The system SHALL isolate Image Studio input-storage failure from unrelated API traffic while preventing tasks from being accepted or claimed without durable shared storage.

#### Scenario: Storage probe fails
- **WHEN** the service cannot create, read, and delete a probe file under the configured Image Studio input root
- **THEN** new Image Studio asynchronous jobs receive a clear 503 response and the Worker pauses task claims while unrelated APIs remain available

### Requirement: Protocol-correct asynchronous execution
The asynchronous Worker SHALL reconstruct the upstream edit request from stored files using the protocol required by the selected account.

```

Full source: openspec/changes/refactor-image-studio-edit-storage/specs/image-studio-edit-jobs/spec.md

