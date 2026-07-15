# 首 Token 超时切换验收报告

日期：2026-07-15
Change：`configurable-first-token-timeout-failover`
结论：通过，等待分支处理决定。

## 需求对照

- 管理员可以独立配置首 Token 超时开关和 1-300 秒阈值；配置读取失败时默认关闭，保存失败时继续使用旧快照。
- 三个目标流式协议在首个语义 token 前均启用 attempt gate；超时立即取消上游、回滚未提交输出并切换账号。候选耗尽返回稳定的 `504 first_token_timeout`。
- 超时 attempt 不产生正常用量或余额扣费；客户端取消、非流式、WebSocket、图片、视频和批处理均不进入该流程。
- 新增按小时聚合的 TTFT 表、异步 recorder、请求和账号两个统计口径，以及管理员 `/admin/ttft` 页面和 API。
- 全部 29/29 OpenSpec 任务和 67/67 实施计划任务已标记完成；实现保留现有 `first_token_ms` 逻辑，采用追加式低冲突接入。

## 本次验证

| 检查 | 结果 |
| --- | --- |
| `make build` | 通过 |
| `go test ./... -count=1` | 通过 |
| `go test -race ./internal/service ./internal/handler -count=1` | 通过 |
| `go vet ./internal/service ./internal/handler ./internal/repository` | 通过 |
| `pnpm run test:run` | 通过，165 个文件、1140 个测试 |
| `pnpm typecheck` | 通过 |
| `openspec validate --type change configurable-first-token-timeout-failover --json` | 通过，1/1 |
| `git diff --check` | 通过 |
| 凭据模式扫描 | 未发现新增明文密钥 |

独立代码审查未发现 Critical 或 Important 问题。PostgreSQL/Redis Testcontainers 统计集成测试已在 Colima socket 环境通过。

## 残余风险

- 尚未使用真实管理员会话执行 `/admin/ttft` 浏览器截图验证；该页面已通过组件挂载、路由、API 和 Chart.js 配置测试。发布前可在目标环境登录管理员账号进行一次人工页面核对。
- 前端全量测试输出既有 `router-link` 解析和 jsdom 网络警告，但测试命令退出码为 0，且与本变更无直接关联。
