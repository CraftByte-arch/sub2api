## 1. 设置与管理 API

- [x] 1.1 新增 `FirstTokenTimeoutSettings`、独立 setting key、默认值、1-300 秒解析校验和保存逻辑，并覆盖缺失/损坏配置回退测试
- [ ] 1.2 新增管理员读取与更新 API、DTO、路由和设置审计字段，覆盖有效保存、非法阈值和关闭状态测试
- [x] 1.3 新增只读策略快照供每个 eligible attempt 获取启用状态与阈值，保证保存热更新、并发读取和损坏配置回退不阻塞网关

## 2. Attempt 控制器与事务化响应门

- [x] 2.1 为 pending、committed、timed_out、canceled 状态编写并发与竞态测试，明确只有一个终态转换能成功
- [x] 2.2 实现基于 `context.WithCancelCause` 的 attempt controller、timer 生命周期和 context helper，确保 commit、正常结束及客户端取消均释放资源
- [x] 2.3 实现完整 `gin.ResponseWriter` 契约的事务化 writer，支持本地 headers、Write/WriteString、抑制 Flush、Commit 和 Rollback
- [x] 2.4 实现 256 KiB prelude 缓冲上限及溢出 failover，并覆盖 header 不泄漏、事件顺序、接口兼容和缓冲回收测试

## 3. 协议语义 token 判定与接入

- [x] 3.1 为 OpenAI Responses、Chat Completions 和 Anthropic Messages 建立纯函数 token detector 测试，覆盖文本、reasoning、工具调用、metadata、role-only、usage、ping 和空 delta
- [x] 3.2 接入 OpenAI Responses HTTP 流式路径，在首个语义 token 前门控输出，并保持 compact keepalive、silent refusal 和既有错误分类行为
- [x] 3.3 接入 OpenAI Chat Completions HTTP 流式路径，确保 role-only chunk 不提交、内容或工具 delta 正确提交
- [x] 3.4 接入 Anthropic Messages HTTP 流式路径，确保 lifecycle/keepalive 不提交、内容或工具输入增量正确提交
- [x] 3.5 验证首 token 提交后关闭 TTFT 控制，后续停流仍由现有 stream data interval timeout 处理

## 4. Failover、调度与计费

- [x] 4.1 将 attempt timeout 和 prelude overflow 转换为稳定的 typed `UpstreamFailoverError`，TTFT 超时使用 504、`first_token_timeout` 且禁止同账号重试
- [x] 4.2 在目标 handler failover 循环中按 attempt 安装/回收门控，正确释放账号槽、排除超时账号并受现有 `maxAccountSwitches` 限制
- [x] 4.3 记录安全的 TTFT timeout 结构化事件、Ops 指标和账号调度失败结果，不记录正文、凭据或内部地址
- [x] 4.4 确保超时 attempt 不写正常 usage log、不扣除 Sub2API 用户余额，最终成功 attempt 继续正常计费

## 5. 独立统计存储与查询

- [ ] 5.1 新增 `first_token_timeout_stats_hourly` migration，定义 attempt/request scope、维度哨兵、outcome/failure kind 约束、加法 UPSERT 唯一键和 90 天查询索引，并增加迁移测试
- [ ] 5.2 新增独立 stats port/repository，实现批量原子 UPSERT、汇总/趋势/失败分类/账号分页查询和 90 天幂等清理，覆盖多实例累加与阈值快照测试
- [ ] 5.3 新增有界异步 recorder，支持非阻塞 Record、5 秒/批量阈值 flush、2 秒停机 flush、dropped count、last successful flush 和每日清理，覆盖 DB 失败不传播及竞态测试
- [ ] 5.4 在 attempt 与 request 生命周期末端各记录一次 outcome，统一其他失败分类，确保 client_canceled 排除率分母、TTFT 后其他失败仍进入受影响 request 分母
- [ ] 5.5 新增管理员 TTFT summary/trend/failure-distribution/account-stats API、DTO、参数校验和 completeness 元数据，覆盖 24h/7d/30d/90d、协议/模型与账号局部筛选

## 6. 独立管理员页面

- [ ] 6.1 新增 `/admin/ttft` 路由、侧边栏“首 Token 监控”入口、独立前端 API/types 和中英文 locale，设置筛选状态同步 URL
- [ ] 6.2 页面顶部实现策略加载、toggle、1-300 秒输入、保存按钮和生效/校验状态，不再修改现有大型 SettingsView 区块
- [ ] 6.3 实现五项汇总指标、失败率趋势折线图、其他失败分类横向条形图和 completeness 提示，所有比例显示分子/分母
- [ ] 6.4 实现账号统计表的搜索、平台/账号筛选、排序、分页、平均 TTFT 与低样本提示，并覆盖 skeleton、空态、错误重试、暗色和响应式状态
- [ ] 6.5 增加页面/API 单元测试，验证全局 request 筛选不受账号局部筛选影响、URL 恢复、保存设置和 degraded 状态

## 7. 端到端验证与发布保护

- [ ] 7.1 增加慢账号超时后第二账号成功、候选耗尽返回 504、客户端取消停止重试、失败账号输出完全不可见且统计 outcome 正确的集成测试
- [ ] 7.2 增加关闭功能、非流式、WebSocket、图片/视频/批处理不受影响且不产生统计样本的回归测试
- [ ] 7.3 运行后端目标包与全量测试、迁移测试、前端类型检查/测试/生产构建，并记录默认关闭发布、短阈值启用、统计完整性和设置开关回滚验证结果
