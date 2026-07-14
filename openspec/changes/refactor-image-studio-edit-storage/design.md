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

1. 查询到期且不处于 running 状态的输入记录，删除目录并更新状态。
2. 扫描 `inputs/` 下超过安全宽限期且数据库无引用的目录并删除。
3. 保持现有输出文件清理。

API Key multipart body 使用任务目录中的短生命周期 spool 文件构造并流式发送，避免最多四张图片在高并发下长期占用内存。spool 请求结束立即删除，异常残留由更短宽限期的孤儿扫描清理。

删除操作必须幂等；文件已不存在时仍可成功写入删除时间。数据库更新失败时后续轮次可再次收敛。

### 7. Legacy 数据渐进迁移

数据库迁移增加新字段，并立即从 succeeded/failed 等终态任务的 `request_payload` 删除 `images` 和 `mask`。仍可执行的 legacy 编辑任务暂时保留 data URL；Worker 首次领取时将 1 到 4 张图片和蒙版落盘、更新路径并从 payload 删除二进制字段，然后按新路径执行。

超过 4 张、无有效图片或无法解码的 legacy 任务进入明确失败状态并清除可清除的图片内容。迁移后新建任务不再接受 data URL。

## Risks / Trade-offs

- [本地文件系统在多实例间不可见] → 将共享持久化 `DATA_DIR` 作为部署前提，并在启动/健康检查中验证目录可写。
- [文件系统与数据库无法组成同一事务] → 使用临时文件、原子 rename、失败回滚和孤儿目录扫描实现最终收敛。
- [前端 WebP 压缩可能增加客户端 CPU/内存] → 最多并行处理 4 张并保持现有质量策略；压缩失败直接停止提交。
- [输入 TTL 到期时任务仍在运行] → 清理器跳过 running，Worker 在完成路径上负责删除。
- [路径字段被污染可能导致越界删除] → 只保存相对路径并对所有 resolve/read/remove 操作执行根目录约束。
- [输入存储不可写或挂载缺失] → 启动探针将 Image Studio 异步子系统标记 unavailable，新建任务返回 503 且 Worker 暂停领取，不影响其他网关 API。
- [旧版本应用无法执行新 path-only 任务] → 采用 schema-first 部署并优先 roll forward；必要回滚时先暂停编辑任务创建并排空新任务。
- [清理历史 JSONB 后表文件不会立即缩小] → 停止新增膨胀后由 PostgreSQL autovacuum 回收可复用空间，运维窗口再决定是否 `VACUUM FULL`。

## Migration Plan

1. 部署兼容性数据库迁移：添加输入路径和生命周期字段，清理终态 payload 中的图片字段，不删除旧列。
2. 部署可同时读取 path-only 和 legacy data URL 的后端；启动时验证输入根目录。
3. 部署 multipart 前端，停止创建新的 data URL 编辑任务。
4. 观察 legacy 活跃任务完成或失败，确认数据库 payload 不再增长且输入目录按状态清理。
5. 在后续独立清理窗口评估数据库物理压缩，不把锁表操作放入应用迁移。

回滚优先采用 roll forward。若必须回滚，在旧版本启动前暂停 Image Studio 编辑入口并等待 path-only 任务处理完成，避免旧 Worker 无法读取新字段。

## Open Questions

无。
