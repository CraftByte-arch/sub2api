# Image Studio Edit Input Storage Operations

本文档适用于 Image Studio 异步编辑任务的文件输入存储。输入文件位于
`DATA_DIR/image-studio/inputs/`，输出文件继续位于 `DATA_DIR/image-studio/<job-id>/`。
当前方案只支持共享持久化文件系统，不使用 S3 或其他对象存储。

## Deployment Order

必须按以下顺序发布：

1. 先应用 `175_image_studio_edit_input_storage.sql`。该 migration 添加 `input_image_paths`、
   `input_mask_path`、`input_expires_at`、`input_deleted_at` 和清理索引，并清除终态
   legacy edit payload 的 `images`、`mask` 字段。
2. 挂载共享持久化 `DATA_DIR`，再发布能够同时读取 path-only 和 active legacy data URL
   的后端。所有接收请求和执行 Worker 的实例必须看到同一个文件系统。
3. 等待每个新后端实例的输入存储 probe 变为 healthy。确认新建任务返回正常、Worker
   能领取任务后，才允许进入下一步。
4. 发布 multipart 前端。发布期间建议临时禁用编辑入口，避免旧前端在新后端上提交已被
   拒绝的 JSON data URL edit 请求。
5. 观察 active legacy backlog、path-only 任务、磁盘和数据库增长。确认所有旧 Worker
   已退出后，才能持续接收 multipart edit 请求。

仓库没有独立的 migration CLI。应用以 `AUTO_SETUP=true` 启动时会调用嵌入式
`repository.ApplyMigrations`；完整 Compose 拓扑的实际 runner 命令和验收查询如下。独立
Compose 拓扑使用相同应用启动命令，但验收查询须对外部 PostgreSQL 执行。

```sh
docker compose -f deploy/docker-compose.yml up -d sub2api
docker compose -f deploy/docker-compose.yml exec -T postgres \
  psql -U "${POSTGRES_USER:-sub2api}" -d "${POSTGRES_DB:-sub2api}" -Atc \
  "SELECT filename FROM schema_migrations WHERE filename = '175_image_studio_edit_input_storage.sql'" \
  | grep -Fx '175_image_studio_edit_input_storage.sql'
```

蓝绿发布时，绿色后端必须先完成 migration、共享卷和 probe 检查。旧版本不能执行新建的
path-only 任务，因此在打开 multipart 前端流量前必须从队列消费路径移除蓝色 Worker。
不要让旧 Worker 和新 multipart 前端同时工作。

## Shared DATA_DIR

每个 API receiver 和 Image Studio Worker 实例必须以相同内容挂载 `/app/data`，并且对
`/app/data/image-studio` 具备读、写、创建、rename、fsync 和删除权限。只在单机上创建的
本地目录不满足多实例要求。

仓库现有 Compose 文件已经为应用容器挂载 `/app/data`：

- `deploy/docker-compose.yml`: `sub2api_data:/app/data`
- `deploy/docker-compose.standalone.yml`: `sub2api_data:/app/data`
- `deploy/docker-compose.local.yml`: `./data:/app/data:Z`
- `deploy/docker-compose.dev.yml`: `./data:/app/data:Z`

命名卷在同一 Docker host 内可复用，但不会自动跨主机共享。多主机部署必须把同一共享
持久化文件系统挂载到每个实例的 `/app/data`；不能为每个主机创建同名但内容独立的卷。

发布前在每个实例执行以下检查。命令只输出状态和元数据，不读取用户文件内容：

```sh
DATA_DIR=${DATA_DIR:-/app/data}
test -d "$DATA_DIR/image-studio" || mkdir -p "$DATA_DIR/image-studio"
test -r "$DATA_DIR/image-studio" && test -w "$DATA_DIR/image-studio"
stat -c 'owner=%u:%g mode=%a' "$DATA_DIR/image-studio"
df -Pk "$DATA_DIR/image-studio"
df -Pik "$DATA_DIR/image-studio"
```

应用启动时会立即执行 input storage probe，并只在状态转换时记录日志；通用 `/health`
不能替代该 gate。以下命令要求当前 Compose service 的每个实例在本次发布窗口记录过
`healthy=true`，任一实例缺失即返回非零：

```sh
for container in $(docker compose -f deploy/docker-compose.yml ps -q sub2api); do
  docker logs --since 10m "$container" 2>&1 \
    | grep -E 'image_studio_input_storage_health_transition.*healthy=true' >/dev/null \
    || { echo "input storage probe not healthy for container=$container" >&2; exit 1; }
done
```

容器应以稳定的 UID/GID 访问共享卷。修复权限时只修改 Image Studio 数据根，不要递归
改变不相关数据。SELinux 环境保留 Compose 的 `:Z` 标记或使用符合站点策略的共享标签。

## Retention And Cleanup

输入 TTL 默认是 24 小时，由 `image_studio_input_retention_hours` 独立控制。它只决定排队、
失败和重试所需输入的最长保留时间。输出仍使用已有的
`image_studio_retention_value`/`image_studio_retention_unit` 策略和
`expires_at`/`assets_deleted_at` 状态；延长输出保留不会延长输入 TTL。

- 成功任务只有在 output assets 和 settlement recovery payload 持久化后才删除输入。
- 可重试失败在 TTL 内保留输入。
- 到期 queued 任务原子变为 `input_expired`，随后删除输入。
- running 任务不会被清理器中途删除；离开 running 路径后由 Worker 收敛。
- 用户删除任务时先删除输入和输出目录，再删除数据库记录。
- 无数据库引用且超过 1 小时宽限期的 upload 目录会被有界清理。
- API Key multipart spool 正常请求结束即删除；残留 `.spool-*.multipart` 超过 10 分钟
  后进入有界清理。

## Monitoring

至少监控以下信号：

- `image_studio_input_storage_health_transition`: 按实例统计 healthy/false transitions；
  false 时新任务返回 503，Worker 暂停领取，但无关 API 应保持可用。
- Active legacy materialization: 定时记录仍含 legacy binary fields 且没有 path 的活跃任务数，
  该数应单调收敛到 0。
- Terminal input codes: `input_invalid`、`input_expired`、`legacy_input_invalid`、
  `input_path_invalid`、`input_missing`、`input_storage_unavailable` 的速率和积压。
- Cleanup failures: `image_studio_input_cleanup_query_failed`、
  `image_studio_input_cleanup_failed`、`image_studio_input_orphan_cleanup_failed`、
  `image_studio_input_expiry_transition_failed`。
- Multipart cleanup failures: `image_studio_multipart_spool_cleanup_failed`。
- Orphan/spool backlog: upload 目录数、spool 文件数、最老文件年龄和每轮删除量。
- Database growth: `request_payload` 含 binary keys/data URL 的行数、payload 总大小、表和索引
  大小。新增 edit row 的 binary count 必须保持 0。
- Disk: `/app/data` 容量、inode、Image Studio inputs/outputs 分别占用和增长速度。

应用日志只能记录 job ID、阶段、计数和稳定 `error_kind`。不要记录文件 bytes、data URL、
base64、用户 prompt、绝对/相对输入路径或 spool 路径。排障命令也不要 `cat`、`head`、
`find -print` 用户文件。

## Safe Database Checks

以下 SQL 只返回计数和大小，不返回 `request_payload` 或路径内容：

```sql
-- New and legacy binary payload inventory. Target for non-active rows is zero.
SELECT
  COUNT(*) FILTER (
    WHERE request_payload ? 'images'
       OR request_payload ? 'mask'
       OR request_payload::text LIKE '%data:image/%'
  ) AS rows_with_binary_payload,
  COUNT(*) FILTER (
    WHERE mode = 'edit'
      AND status IN ('queued', 'running', 'settling')
      AND input_image_paths = '[]'::jsonb
      AND (request_payload ? 'images' OR request_payload ? 'mask')
  ) AS active_legacy_rows,
  COUNT(*) FILTER (
    WHERE mode = 'edit'
      AND jsonb_array_length(input_image_paths) BETWEEN 1 AND 4
  ) AS path_backed_edit_rows
FROM image_studio_jobs;

-- Path cardinality and lifecycle counts without exposing path values.
SELECT
  status,
  jsonb_array_length(input_image_paths) AS image_count,
  (input_mask_path IS NOT NULL) AS has_mask,
  (input_deleted_at IS NOT NULL) AS inputs_deleted,
  COUNT(*) AS jobs
FROM image_studio_jobs
WHERE mode = 'edit'
GROUP BY status, image_count, has_mask, inputs_deleted
ORDER BY status, image_count, has_mask, inputs_deleted;

-- Terminal input failures. Do not select error_message or payload fields.
SELECT error_code, COUNT(*) AS jobs
FROM image_studio_jobs
WHERE error_code IN (
  'input_invalid', 'input_expired', 'legacy_input_invalid',
  'input_path_invalid', 'input_missing', 'input_storage_unavailable'
)
GROUP BY error_code
ORDER BY error_code;

-- Payload and physical table growth.
SELECT
  COUNT(*) AS rows,
  COALESCE(SUM(pg_column_size(request_payload)), 0) AS payload_bytes,
  pg_total_relation_size('image_studio_jobs') AS table_and_index_bytes
FROM image_studio_jobs;
```

对新发布窗口增加时间条件时使用 `created_at >= :deployment_time`，仍只返回聚合结果。

## Safe Filesystem Checks

这些命令不打印文件名或路径列表：

```sh
DATA_DIR=${DATA_DIR:-/app/data}
ROOT="$DATA_DIR/image-studio"

find "$ROOT/inputs" -mindepth 1 -maxdepth 1 -type d -print0 2>/dev/null \
  | tr -cd '\0' | wc -c
find "$ROOT/inputs" -type f -name '.spool-*.multipart' -print0 2>/dev/null \
  | tr -cd '\0' | wc -c
find "$ROOT/inputs" -type f -printf '%s\n' 2>/dev/null \
  | awk '{sum += $1; count += 1} END {print "files=" count+0, "bytes=" sum+0}'
du -sk "$ROOT/inputs" 2>/dev/null | awk '{print "input_kib=" $1}'
du -sk "$ROOT" 2>/dev/null | awk '{print "image_studio_total_kib=" $1}'
```

验证一个已成功任务的输出仍可下载时，只检查 HTTP 状态，不把响应写到终端：

```sh
curl -fsS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/api/v1/image-studio/jobs/$JOB_ID/original"
```

## Failure Recovery

输入存储 probe unhealthy 时：

1. 保持编辑入口关闭，不强制 Worker 领取任务。
2. 在每个实例检查挂载是否存在、是否同一共享内容、UID/GID、读写权限、空间和 inode。
3. 修复卷或权限后等待 probe 从 false 转为 true；确认只恢复一次正常领取。
4. 查询 `input_storage_unavailable`、`input_missing` 和 active legacy backlog，区分挂载故障、
   数据丢失与单行污染。
5. 对缺失或越界路径保持终态失败，不手工拼接路径或从日志恢复用户内容。

清理失败时先修复数据库或文件系统，再让后续 30 分钟清理轮次幂等收敛。不要直接删除
running 目录。孤儿和 spool 堆积可在停止相关 Worker 后使用相同根约束和宽限期处理；不要用
不受约束的递归通配符删除 `/app/data`。

## Rollback And Roll-forward

优先 roll-forward。旧版本不能执行 path-only edit row，直接回滚会让任务永久失败或重试。

必须回滚时：

1. 先禁用 Image Studio edit 创建并撤下 multipart 前端。
2. 停止旧版本上线，等待 queued/running/settling path-only 任务排空；用聚合 SQL 确认没有
   未完成 path-backed edit row。
3. 保留 `175_image_studio_edit_input_storage.sql` 添加的字段和共享文件，不回退 schema 或
   删除路径元数据。
4. 只有在 path-only 队列为 0 后才启动旧版本。恢复服务后仍优先发布兼容后端并 roll-forward。

migration 清除 JSONB 字段后，PostgreSQL 表文件不会立即缩小。依赖 autovacuum 回收可复用
空间；可在低峰执行普通 `VACUUM (ANALYZE) image_studio_jobs`。`VACUUM FULL` 会取得强锁并
重写表，只能在明确维护窗口执行，不能放入应用 migration 或自动恢复流程。

## Verification Evidence

本变更的本地受控验证使用以下命令；不连接生产环境：

```sh
# PostgreSQL 18.1 integration harness; Redis 8.4 is started and pinged by TestMain.
CI=true \
DOCKER_HOST=unix:///Users/jixiwen/.colima/default/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
go test -tags=integration ./internal/repository \
  -run '^(TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate|TestImageStudioMigrationRedactsTerminalEditPayloadsAndKeepsActiveLegacyPayloads)$' \
  -count=1 -v

# Real multipart create, Worker protocols, lifecycle cleanup, delete, and retention.
CI=true \
DOCKER_HOST=unix:///Users/jixiwen/.colima/default/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
go test -tags=integration ./internal/repository \
  -run '^TestImageStudioEditStorageReal(CreateAndWorkerProtocols|LifecycleCleanupAndDelete)$' \
  -count=1 -v

# Backend focused and complete verification.
go test ./migrations -run '^TestImageStudioEditInputStorageMigration' -count=1 -v
go test ./internal/repository -run '^(TestImageStudioJobRepository|TestScanImageStudioJob|TestImageStudioMigration)' -count=1 -v
go test ./internal/handler -run '^(TestImageStudio|TestBuildImageStudio|TestCopyImageStudio|TestParseImageStudio)' -count=1 -v
go test ./internal/service -run '^(TestImageStudio|TestBuildOpenAIStoredEdit|TestOpenAIGatewayServiceForwardImagesOAuth|TestIsImageStudio|TestClassifyImageStudio)' -count=1 -v
go test ./... -count=1
go vet ./...

# CI-pinned lint. Full-tree output currently contains documented baseline issues;
# the change-scoped gate must report zero issues.
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.9.0 run ./... --timeout=30m
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.9.0 run ./... \
  --timeout=30m --new-from-rev e15265205a50addfeba66f935b7e256ea2a51f20

# Frontend.
pnpm test:run
pnpm lint:check
pnpm typecheck
pnpm build
```

5.3 的跨组件证据由以下精确测试组合提供：

- 真实 PostgreSQL/migrations、multipart handler/service create、Worker 协议、durable
  settlement recovery payload 与文件生命周期：
  `TestImageStudioEditStorageRealCreateAndWorkerProtocols` 的
  `api key uses ordered four-image multipart spool`、
  `api key one-image output downloads through authenticated handler` 和
  `oauth uses request-local responses edit representation` 子测试，以及
  `TestImageStudioEditStorageRealLifecycleCleanupAndDelete`。
- 新 row payload 无 `images`/`mask`/data URL：
  `TestImageStudioJobServiceCreateEditJobStagesInputsAndSanitizesPayload`、
  `TestImageStudioJobRepositoryCreateWritesOrderedInputMetadata`、
  `TestImageStudioJobHandlerCreateAcceptsOrderedMultipartImages`。
- 1/4 张 API Key repeated multipart 顺序：上述两个 API Key integration 子测试，以及
  `TestImageStudioJobHandlerCreateAcceptsOrderedMultipartImages`、
  `TestImageStudioEditMultipartSpoolPreservesInputsAndMetadata`、
  `TestImageStudioJobServiceForwardSelectedAPIKeyEditAlwaysCleansMultipartSpool`。
- OAuth 继续使用 Responses edit representation：
  `TestOpenAIGatewayServiceForwardImagesOAuthStoredEditUsesResponsesRepresentation` 和
  `TestImageStudioOAuthSettlementPayloadContainsNoInputBytes`。
- success/TTL/delete 删除输入：integration lifecycle 测试通过生产 cleanup loop 验证 TTL，
  并与以下 focused tests 组合：
  `TestImageStudioJobServiceDeletesInputsOnlyAfterSettlingIsDurable`、
  `TestImageStudioJobServiceExpiredRunningFailureBecomesTerminalAndDeletesInputs`、
  `TestImageStudioJobServiceCleanupInputsIsBoundedAndIsolatesFailures`、
  `TestImageStudioJobServiceDeleteJobRemovesAssetsAndRecord`。
- 输出保留与输入删除独立：一图 integration 子测试通过带认证上下文的
  `ImageStudioJobHandler.GetOriginal` 下载输出，并组合
  `TestImageStudioJobRepositoryOutputRetentionIgnoresInputDeletionState`。

真实 PostgreSQL 测试证明 migration/schema、legacy redaction、handler 创建、path-only
数据库行、Worker 协议重建、simple-mode settlement recovery payload、生产 cleanup loop
和文件生命周期；fixture 使用 `RunModeSimple`，因此不证明标准模式余额扣费或 usage receipt
持久化。integration `TestMain` 同时启动并 ping Redis 8.4，但该 Image Studio fixture 没有
注入 Redis-backed dependency，Redis 只证明 harness 可用。实际上游使用 httptest transport
捕获，不连接外部 provider。前端浏览器不在该 Go integration 路径内，由 Image Studio
Vitest 的压缩、multipart 和 1/4 图场景覆盖。
