## 1. 后端统计范围与聚合

- [x] 1.1 为默认 30 天、显式起止日期、浏览器时区、DST 日界线、非法参数和 366 天上限编写失败测试。
- [x] 1.2 实现共享日期范围解析器，生成规范化日期、有效 IANA location 和半开绝对时间区间。
- [x] 1.3 为 `PAID`、`RECHARGING`、`COMPLETED` 状态过滤、用户隔离、缺少 `paid_at`、三种类型补零和每日倒序编写聚合测试。
- [x] 1.4 为小数金额累计与平均舍入编写失败测试，并实现整数分转换和纯聚合函数。
- [x] 1.5 实现最长 366 天的当前用户有界字段查询，返回固定 CNY 的单一 `summary`、三行 `by_type` 和 `daily`。

## 2. 用户统计与下钻 API

- [x] 2.1 为未认证请求、有效统计查询、用户 ID 强制取自认证主体、固定 CNY 响应和服务错误映射编写 handler 测试。
- [x] 2.2 新增 `GET /api/v1/payment/orders/statistics` handler 和认证静态路由，并验证其不受 `/:id` 路由影响。
- [x] 2.3 为下钻选择器互斥、非法类型/日期、范围外日期、固定每页 20 条和稳定排序编写失败测试。
- [x] 2.4 新增 `GET /api/v1/payment/orders/statistics/details`，使用数据库计数和分页并返回最小只读 DTO。
- [x] 2.5 增加一致性测试，验证类型/日期聚合的 `order_count` 与对应下钻 `total` 相等且不会泄露其他用户数据。

## 3. 独立订单统计页面

- [x] 3.1 增加统计与下钻请求/响应类型、payment API 方法及参数和固定分页契约测试。
- [ ] 3.2 为新页面编写组件测试，覆盖默认 30 天、7/30/90 天快捷范围、自定义 draft/applied 状态、加载、错误、重试和空状态。
- [ ] 3.3 实现 `UserOrderStatisticsView.vue`，展示人民币总实付、成功订单数、平均实付、三种类型聚合和按日列表。
- [ ] 3.4 增加 `/order-statistics` 路由、支付 feature flag 保护、相邻侧边栏入口和中英文文案，并测试菜单可见性与激活状态。

## 4. 聚合下钻弹窗

- [x] 4.1 为类型行与每日行的鼠标/键盘下钻、弹窗上下文、关闭重开和页码重置编写组件测试。
- [x] 4.2 实现整行可操作的类型/每日列表，以及使用 `BaseDialog`、`DataTable` 和固定每页 20 条 `Pagination` 的只读明细弹窗。
- [x] 4.3 覆盖弹窗加载、空状态、内部错误重试、翻页以及订单号、类型、金额、状态、支付方式和支付时间展示。
- [ ] 4.4 为页面与弹窗加入独立请求代次防护，测试较晚返回的旧请求在范围切换、下钻切换、翻页或关闭后被忽略。

## 5. 回归验证与交付

- [ ] 5.1 增加回归断言，确认 `UserOrdersView.vue`、`/orders` 和 `GET /api/v1/payment/orders/my` 未被修改。
- [ ] 5.2 运行 payment service、handler、路由/API 契约相关 Go 测试及 race/vet 检查。
- [ ] 5.3 运行新增页面 Vitest、前端 lint、typecheck 和生产构建。
- [ ] 5.4 在桌面和移动视口做统计页视觉检查，覆盖长订单号、长日期、空状态和弹窗表格横向滚动，确认无溢出、重叠或布局跳动。
- [ ] 5.5 运行 `openspec validate add-my-orders-payment-statistics --type change --strict --no-interactive` 和 `git diff --check`，记录验证结果。
