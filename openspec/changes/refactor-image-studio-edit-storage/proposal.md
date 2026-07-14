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
