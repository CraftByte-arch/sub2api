# Comet Design Handoff

- Change: image-studio-async-queue
- Phase: design
- Mode: compact
- Context hash: f40920ea2d79c68c97417fa73c9cb77b5e1e6d54200a22ee570d39a1aa3ab00a

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/image-studio-async-queue/proposal.md

- Source: openspec/changes/image-studio-async-queue/proposal.md
- Lines: 1-25
- SHA256: bae13122caaaf73d99539c02a2ba8dcc757054aa152e75193ccb9798854b8d42

```md
## Why

后台 `image-studio` 当前直接调用同步生图接口，容易在上游限流或瞬时高并发时失败，也无法由服务端统一控制总体并发。需要新增一套仅供后台站点使用的异步任务队列，提升成功率并保留现有对外 API 的兼容性。

## What Changes

- 新增后台专用异步生图任务接口，用于提交、查看列表和查看详情，不修改现有对外同步生图接口。
- 新增持久化任务模型、后台 worker 和“每用户 FIFO + 全局并发上限”调度机制。
- 新增后台生图预扣费、成功核销、失败退款的任务结算流程。
- 新增本地文件落盘的原图与缩略图存储、详情按需加载原图、前端缓存原图与下载复用缓存的交互模式。
- 新增全站统一的图片保留时长设置，支持小时/天两个单位，`0` 表示永不过期，并由后台定时清理过期图片文件。

## Capabilities

### New Capabilities
- `image-studio-async-jobs`: 后台站内异步生图任务的提交、排队、执行、计费、存储、查询与过期清理能力。

### Modified Capabilities
- 无

## Impact

- 影响后台 Go 服务中的管理端接口、任务调度、计费、文件存储与定时清理逻辑。
- 影响前端 `frontend/src/extensions/image-studio/*`，从同步请求改为异步任务提交与轮询。
- 不影响现有 `/v1/images/generations`、`/v1/images/edits` 及其他对外 API 契约。
```

## openspec/changes/image-studio-async-queue/design.md

- Source: openspec/changes/image-studio-async-queue/design.md
- Lines: 1-54
- SHA256: 9179cbea25e92da01ebd7e1824f98e3a2fae2c1a7c9469b3bce45f83245a893e

```md
## Context

当前后台 `image-studio` 是浏览器直接调用现有同步生图接口并在本地保存历史记录。这种模式对外 API 复用简单，但无法由服务端统一控制生图峰值并发，也无法为后台用户提供持久化任务列表、稳定的失败补偿和过期清理。项目当前是单实例部署，首期可以接受本地文件存储。

## Goals / Non-Goals

**Goals:**
- 为后台站点新增一套不影响现有公网 API 的异步生图任务入口。
- 提供持久化任务状态、每用户 FIFO、公平调度和全局并发控制。
- 在任务提交时完成预扣费，失败时自动退款。
- 为任务列表提供缩略图，为详情页提供按需加载原图。
- 支持管理员设置全站统一的图片保留时长，并清理过期文件。

**Non-Goals:**
- 不修改现有 `/v1/images/generations`、`/v1/images/edits` 的行为或契约。
- 不在首期支持单任务多图、任务取消、推送通知或对象存储。
- 不把该任务系统抽象成项目内通用作业平台。

## Decisions

- **新增后台专用异步接口，而非改造现有同步接口。**
  这样可以完全隔离后台站内需求与对外 API 契约，避免对现有外部调用方产生行为回归。

- **采用数据库持久化任务表 + 应用内 worker。**
  相比纯内存队列，该方案支持服务重启恢复与任务列表查询；相较引入 Redis 作业系统，首期依赖更少，更贴合现有单体服务结构。

- **使用单表保存单任务单张图的全部核心字段。**
  当前后台不支持一次生成多张图，没有必要增加结果子表与额外关联复杂度。

- **调度策略固定为“每用户 FIFO + 全局最大并发”。**
  worker 只允许每个用户最早的一个 `queued` 任务进入候选，再按 `queued_at` 竞争全局运行槽位，避免单个用户占满队列。

- **文件首期落本地目录，并由受控接口提供访问。**
  当前是单实例部署，可以先使用本地磁盘；通过后台详情/图片接口屏蔽底层路径，为未来切换对象存储留接口边界。

- **任务列表只返回缩略图，原图在详情中按需请求。**
  这样可以显著降低列表页面加载成本，并配合前端原图缓存实现更稳定的预览和下载体验。

- **保留时长在任务成功时换算为 `expires_at`。**
  管理员设置使用“数值 + 单位（小时/天）”，但清理程序只依赖确定性的 `expires_at` 字段，减少运行时分支判断。

## Risks / Trade-offs

- **[单实例限制]** 本地文件存储与应用内 worker 假设当前部署为单实例。  
  **Mitigation:** 通过存储与任务执行边界抽象，为后续迁移到共享存储或对象存储保留演进空间。

- **[预扣与退款一致性]** 任务失败、崩溃恢复或重复执行时，容易出现重复退款或漏退款。  
  **Mitigation:** 使用独立的预扣标识和幂等结算逻辑，状态流转与账务操作尽量放在同一事务边界。

- **[运行中任务卡死]** 进程异常可能留下长时间 `running` 的僵尸任务。  
  **Mitigation:** worker 定时写心跳，服务启动与周期巡检时回收心跳超时任务。

- **[前端缓存陈旧]** 原图被后端清理后，浏览器可能仍保留缓存。  
  **Mitigation:** 详情接口返回资源状态；若后端标记资源已删除，前端主动丢弃本地缓存。
```

## openspec/changes/image-studio-async-queue/tasks.md

- Source: openspec/changes/image-studio-async-queue/tasks.md
- Lines: 1-29
- SHA256: 3650c010e6a05b90e5031bca48a9c632a4c161ee14e2607158b905a6335d1252

```md
## 1. Backend job model and settings

- [ ] 1.1 Add persistent image studio job schema, status fields, asset metadata, retention metadata, and billing hold linkage.
- [ ] 1.2 Add global admin settings for image studio retention value/unit and global async concurrency.
- [ ] 1.3 Add repository and service primitives for job creation, status updates, stale-job recovery, and cleanup scanning.

## 2. Worker execution and billing

- [ ] 2.1 Implement backend worker polling with per-user FIFO candidate selection and global concurrency enforcement.
- [ ] 2.2 Implement job execution flow for text-to-image and image-to-image using existing image generation services through internal service boundaries.
- [ ] 2.3 Implement pre-charge hold creation, success finalization, failure refund, and idempotent recovery behavior.

## 3. Asset storage and retention

- [ ] 3.1 Implement local original image storage and thumbnail generation for successful jobs.
- [ ] 3.2 Add backend asset access endpoints for thumbnails and originals with resource-state checks.
- [ ] 3.3 Implement scheduled cleanup for expired local assets while preserving job records and metadata.

## 4. Backend API surface

- [ ] 4.1 Add backend-only async image studio job submit endpoint without changing public synchronous image endpoints.
- [ ] 4.2 Add paginated job list and job detail endpoints that separate thumbnail summaries from original image access.
- [ ] 4.3 Add API validation, permission checks, and failure responses for unsupported payloads or insufficient balance.

## 5. Frontend image studio integration

- [ ] 5.1 Refactor image studio submission flow to create async jobs and poll backend job list/detail endpoints.
- [ ] 5.2 Replace browser-only history with backend-backed job history that shows thumbnails and asset-expired states.
- [ ] 5.3 Cache original images on the frontend for preview reuse and download without refetching when still valid.
```

## openspec/changes/image-studio-async-queue/specs/image-studio-async-jobs/spec.md

- Source: openspec/changes/image-studio-async-queue/specs/image-studio-async-jobs/spec.md
- Lines: 1-93
- SHA256: 7424785db823c36ad8e2c00b7aaa04bea5979a3b7730ae5bebdcafeea7ac12b3

[TRUNCATED]

```md
## ADDED Requirements

### Requirement: Backend image studio jobs are submitted asynchronously
The system SHALL provide backend-only endpoints for image studio users to submit asynchronous image generation jobs without changing the behavior of existing public synchronous image generation APIs.

#### Scenario: Submit text-to-image job
- **WHEN** an authenticated backend image studio user submits a valid text-to-image request
- **THEN** the system creates a persisted job in `queued` state
- **AND** the system returns a job identifier instead of waiting for image generation to finish

#### Scenario: Submit image-to-image job
- **WHEN** an authenticated backend image studio user submits a valid image-to-image request with required reference files
- **THEN** the system creates a persisted job in `queued` state
- **AND** the system stores enough request metadata to execute the job later

#### Scenario: Existing public image endpoints remain unchanged
- **WHEN** an external API client calls an existing public synchronous image generation endpoint
- **THEN** the request is handled by the existing synchronous flow
- **AND** no backend asynchronous job is created implicitly

### Requirement: Jobs are scheduled with per-user FIFO and global concurrency control
The system SHALL execute image studio jobs using per-user FIFO ordering and a configurable global concurrency limit.

#### Scenario: Same user jobs preserve submission order
- **WHEN** the same user has multiple queued jobs
- **THEN** only that user's earliest queued job is eligible to run first
- **AND** later jobs for the same user remain queued until the earlier job leaves `queued` or `running`

#### Scenario: Global concurrency limit is enforced
- **WHEN** the number of running image studio jobs reaches the configured global maximum
- **THEN** additional eligible jobs remain in `queued` state
- **AND** no new job starts until a running job finishes or fails

#### Scenario: Stale running job is recovered
- **WHEN** a running job no longer updates its heartbeat within the configured timeout window
- **THEN** the system marks that running execution as stale
- **AND** the job becomes eligible for recovery according to the worker recovery policy

### Requirement: Image studio jobs use pre-charge billing with failure refund
The system SHALL reserve the user's charge at submission time, finalize it on success, and refund it on failure.

#### Scenario: Submission reserves cost
- **WHEN** a backend image studio user submits a job with sufficient available balance
- **THEN** the system creates a billing hold for the estimated job cost before the job is queued

#### Scenario: Failure refunds reserved cost
- **WHEN** a queued or running image studio job ends in `failed`
- **THEN** the system releases or refunds the reserved billing hold exactly once
- **AND** the job records the failure reason

#### Scenario: Success finalizes reserved cost
- **WHEN** an image studio job ends in `succeeded`
- **THEN** the system finalizes the reserved billing hold exactly once
- **AND** the user is not charged a second time for the same job

### Requirement: Successful jobs expose thumbnails in lists and originals on demand
The system SHALL store one original image and one thumbnail per successful job, show thumbnails in job listings, and provide original images only when requested by detail views.

#### Scenario: Job list returns thumbnail metadata
- **WHEN** a backend image studio user requests the job list
- **THEN** the system returns each job's summary status and thumbnail metadata if assets are available
- **AND** the system does not require the client to download the original image for list rendering

#### Scenario: Job detail returns original image access
- **WHEN** a backend image studio user requests a successful job's detail
- **THEN** the system returns metadata required to fetch the original image
- **AND** the original image is not eagerly included in job list payloads

#### Scenario: Failed job has no image assets
- **WHEN** a job finishes in `failed`
- **THEN** the system does not expose thumbnail or original image assets for that job

### Requirement: Image assets expire according to global retention settings
The system SHALL support a global administrator-configured retention duration for image studio assets using hour or day units, with `0` meaning no automatic expiration.

#### Scenario: Successful job gets computed expiration
- **WHEN** a job succeeds and the global retention setting is greater than zero
- **THEN** the system computes and stores an `expires_at` timestamp for that job's assets

#### Scenario: Zero retention disables expiration
```

Full source: openspec/changes/image-studio-async-queue/specs/image-studio-async-jobs/spec.md

