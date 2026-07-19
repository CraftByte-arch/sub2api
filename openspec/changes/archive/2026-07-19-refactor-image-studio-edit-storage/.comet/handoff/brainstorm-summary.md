# Brainstorm Summary

- Change: refactor-image-studio-edit-storage
- Date: 2026-07-14
- 状态: 设计方案已确认

## 已确认事实

- 编辑任务支持 1 到 4 张有序参考图和最多 1 张蒙版。
- 前端保持原分辨率，将参考图转换为质量 0.72 的 WebP，再以 multipart 二进制上传；蒙版不做有损压缩。
- 后端将输入写入持久化 `DATA_DIR`，数据库只保存服务端生成的受控相对路径和生命周期字段，不保存 base64。
- API Key Worker 必须构造 multipart `/v1/images/edits`；OAuth Worker 继续使用 Responses 编辑转换。
- 输入在不再需要上游重放后删除；输出仍按现有输出保留策略提供历史查看。
- 本变更保持为单一 change，因为上传、路径、执行和清理共享同一个文件状态机。
- 失败和排队任务的输入文件安全默认 TTL 为 24 小时，独立于输出文件保留设置；成功任务立即删除输入。
- 活跃 legacy data URL 编辑任务由 Worker 首次领取时自动落盘、更新路径并清除 base64；终态 legacy 任务直接清除图片字段。
- 文件与数据库一致性采用文件系统优先：随机目录暂存、完整校验、原子 rename、随后创建任务；数据库失败立即回滚，崩溃孤儿由定时扫描收敛。
- 组件边界确认为前端压缩上传、Handler 编排、InputStore 文件职责、Repository 元数据职责、Worker 协议重建和 Cleanup 生命周期收敛。
- 数据库使用 `input_image_paths`、`input_mask_path`、`input_expires_at`、`input_deleted_at`；request_payload 只保留非二进制参数。
- 新增独立 `image_studio_input_retention_hours` 设置，默认 24 小时；输出保留设置不变。
- settlement payload 和输出可靠持久化后即可删除输入，即使计费 settlement 仍需重试；删除失败由清理器收敛，不回滚成功结果。
- API Key Worker 使用任务目录临时 spool 构造 multipart 并以文件流发送，避免多图请求常驻内存；OAuth 仅在单次请求内进行有总量上限的 data URL 转换。
- 输入路径/缺失/损坏/过期错误为终态，不进入上游重试；真正的上游临时错误才保留输入重试。
- legacy 活跃任务落盘与路径/payload 更新采用一次数据库更新，崩溃产生的无引用目录由孤儿扫描清理。

## 确认的技术方案

- 前端将最多四张参考图按原尺寸压缩为质量 0.72 的 WebP，通过重复 multipart `image` 字段上传；蒙版保持原始透明内容。
- Handler 通过独立 InputStore 完成验证、随机目录暂存、原子 finalize 和失败回滚，再创建 queued 任务。
- 数据库只保存有序相对路径、蒙版路径、输入过期和删除时间；request payload 不包含图片字节。
- API Key Worker 使用磁盘 spool 构造可流式发送的 multipart；OAuth Worker 仅在请求期间进行有界 data URL 转换。
- 输入默认 TTL 独立设为 24 小时；结果和 settlement recovery 数据可靠落库后立即清理输入，失败删除由后台收敛。
- queued 到期通过条件更新原子终止，running 跳过清理；手动删除、孤儿目录和 spool 都有幂等清理路径。
- 存储探针失败时只停用 Image Studio 异步子系统并返回 503，不影响其他 API；多实例必须共享持久化 DATA_DIR。
- 活跃 legacy data URL 任务首次领取时渐进落盘；终态旧任务直接脱敏。

## 关键取舍与风险

- 多实例未共享 `DATA_DIR` 会导致 Worker 读不到输入。
- 文件系统与 PostgreSQL 不具备跨资源事务，需要原子文件操作、回滚和孤儿清理收敛。
- 旧版本应用无法读取新 path-only 任务，部署和回滚必须有顺序约束。
- API Key 磁盘 spool 会短暂增加磁盘写入，但避免多图高并发常驻内存，并提供稳定 Content-Length。
- Image Studio 存储不可用时选择子系统降级而非整个服务启动失败，需要清晰的 503 和运维告警。

## 测试策略

- 前端覆盖四图压缩、顺序、multipart 和压缩失败。
- 后端覆盖路径约束、上传回滚、API Key multipart、OAuth 转换、重试、TTL、手动删除、孤儿清理和 legacy 迁移。
- 集成验证数据库无 base64、输入生命周期收敛、输出保留不受影响。

## Spec Patch

- 将输入安全默认 TTL 明确为 24 小时，并补充它独立于输出保留设置的验收场景。
- 增加存储不可用时拒绝新任务并暂停 Worker 的场景。
- 增加蒙版尺寸/透明内容校验、spool 清理和 queued 到期原子终止场景。
