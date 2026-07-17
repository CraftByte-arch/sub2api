# 账号性能列表与详情稳定性设计

## 目标

修复 `/admin/performance` 账号表现区域的四个可用性问题，同时保持现有性能采集、指标定义、筛选、排序和分页行为不变：

1. 账号使用名称作为主标识，ID 作为辅助信息。
2. 点击账号详情后不得出现弹层闪退、页面持续不可交互或滚动锁残留。
3. 将含义不直观的“尝试次数”拆分为“成功调用”和“失败调用”。
4. 平台列复用账号管理页的 `PlatformTypeBadge`，不再显示纯文本平台名。

本次改动只扩充现有账号性能查询的展示元数据并调整性能页组件，不新增数据库表、迁移、采集指标或管理端路由。

## 已确认的统计口径

`counters.attempt_count` 表示账号被实际选中并发起上游调用的总次数。客户端主动取消不属于账号失败，因此列表使用以下定义：

```text
可计入调用 = attempt_count - client_canceled_count
成功调用   = success_count
失败调用   = attempt_count - client_canceled_count - success_count
```

所有派生值不得小于零。失败调用与现有 `failure_rate` 使用同一分子，避免列表次数和失败率口径不一致。页面不再显示“尝试次数”，也不重复增加“总调用次数”。

## 接口与查询设计

现有 `GET /admin/performance/accounts` 为每个账号条目增加以下只读展示字段：

```json
{
  "account_name": "OpenAI 主账号 A",
  "account_type": "oauth",
  "auth_mode": "personalAccessToken"
}
```

- `account_name` 用于列表和详情标题。
- `account_type` 是 `PlatformTypeBadge` 的必要类型字段。
- `auth_mode` 是可选字段，用于正确区分 OpenAI OAuth、PAT 和 Agent Identity；其他平台可为空。

Repository 先按当前逻辑在性能聚合表中完成分组、评分、排序和分页，再在聚合结果外层 `LEFT JOIN accounts`。这样账号元数据不会改变样本数量、排序分数或分页总数。

软删除账号仍从 `accounts` 行读取原名称和类型，以便查看历史数据。账号行确实不存在时：

- 名称回退为 `#<account_id>`；
- 类型和认证模式为空；
- 前端显示带平台图标的简化平台徽标，不伪造账号类型。

Spark 影子账号保留自身名称和类型；认证模式优先读取自身凭据，缺失时从其母账号读取。查询只读取徽标所需的非密钥展示字段，不返回完整 `credentials` 或 `extra`。

接口变更保持向后兼容：现有筛选参数和排序键继续有效，分页响应与指标字段含义不变；只追加账号展示字段以及 `success_count`、`failure_count` 两个排序键。

## 列表设计

账号表列顺序调整为：

```text
账号 | 平台/类型 | 健康度 | 可用率 | 失败率 |
P95 TTFT | P95 总耗时 | 成功调用 | 失败调用
```

交互与展示规则：

- 账号名称使用主文字，下一行使用等宽小字显示 `#ID`。
- 账号名称按钮是打开详情的主要键盘焦点，整行仍支持鼠标点击。
- 平台存在账号类型时使用 `PlatformTypeBadge`，传入 `platform`、`type` 和可选 `authMode`。
- 成功调用使用语义绿色文字，失败调用使用语义红色文字；颜色不是唯一含义，列标题仍明确标识结果。
- 两列使用 tabular numerals 和本地化千分位。
- 保留当前横向滚动策略，不压缩延迟和调用数字到不可读宽度。
- “成功调用”和“失败调用”继续使用现有表头排序交互。后端新增稳定排序键 `success_count` 和 `failure_count`，分别映射到 `success_count` 与现有失败表达式；相同计数继续使用 `account_id` 作为稳定次级排序。

## 详情弹窗设计

现有 `PerformanceInvestigationDrawer` 同时自行管理 Teleport、焦点陷阱、`body` 滚动锁和 `#app inert`。用户观察到“弹层闪一下后整个页面无法操作”，而全页不可交互与全局 `inert` 或遮罩未释放的表现一致。当前单元测试只覆盖正常打开和显式关闭，没有覆盖性能页真实选择流程、快速关闭或组件卸载后的全局状态。

详情改为复用项目现有 `BaseDialog`：

- 使用 `width="extra-wide"` 展示现有指标卡、性能趋势和失败分布。
- `PerformanceInvestigationDrawer` 可保留文件名以减少调用方改动，但内部只负责详情内容和 `close/retry` 事件，不再自行 Teleport、监听全局键盘、设置 `body` overflow 或操作 `#app inert`。
- 打开、Escape、关闭按钮、焦点恢复、滚动锁和背景 inert 全部交给 `BaseDialog` 的统一生命周期。
- 弹窗标题显示“账号名称 · #ID”，标题下方使用同一个平台类型徽标。
- 加载失败只替换弹窗内容并提供重试，不关闭弹窗；账号接口失败不影响已经加载的总览。
- 切换时间范围或平台筛选时继续关闭当前详情并废弃过期请求。

本次不修改全局 `BaseDialog` 或 `modalBodyLock` 的公共行为，避免影响项目内其他弹窗。只有在新增回归测试证明公共组件本身存在缺陷时，才允许对共享实现做最小修复。

## 数据流

```text
性能账号聚合
  -> LEFT JOIN 账号展示元数据
  -> /admin/performance/accounts
  -> PerformanceAccountTable
       名称 + ID
       PlatformTypeBadge
       成功调用 / 失败调用

点击账号
  -> selectedAccount
  -> BaseDialog 打开
  -> /admin/performance/investigation?account_id=<id>
  -> 加载态 / 指标与图表 / 错误重试
  -> 关闭或筛选变化
  -> BaseDialog 统一释放焦点、inert 与滚动锁
```

## 错误与兼容处理

- 账号元数据缺失不能导致整页查询失败；历史聚合仍必须返回。
- `account_type` 或 `auth_mode` 是未知值时不得强制转换成错误的认证类型。
- 失败调用派生使用 `max(0, ...)`，防御旧数据或部分聚合导致的计数不变量异常。
- 详情请求使用现有 generation 机制忽略过期响应；关闭后返回的旧响应不得重新打开弹窗。
- 本次不改变可用率、失败率、健康分、TTFT 直方图或低样本阈值。

## 测试与验收

后端测试：

- Repository 查询返回普通账号名称、类型和认证模式。
- Spark 影子账号可回退读取母账号认证模式。
- 软删除账号仍显示历史名称；缺失账号使用 `#ID` 回退。
- 元数据关联不改变账号聚合值、排序和分页总数。
- `success_count` 与 `failure_count` 排序键按预期排序，并使用账号 ID 稳定打破同值排序。

前端测试：

- 账号名称和 `#ID` 同时显示，平台列渲染 `PlatformTypeBadge`。
- 成功调用等于 `success_count`。
- 失败调用等于 `attempt_count - client_canceled_count - success_count`，并在异常输入下不小于零。
- 从真实账号按钮打开详情、关闭、快速重复打开/关闭和组件卸载后，`#app` 不残留 `inert`，`body` overflow 恢复，页面按钮仍可点击。
- 详情加载失败后弹窗保持可操作，重试和关闭均有效。
- 账号详情标题显示名称和 ID。

验证命令包括后端相关 Go 测试、性能页 Vitest、前端类型检查和生产构建。最终在桌面与移动宽度、浅色与深色主题下检查表格横向滚动、徽标高度、弹窗边界、关闭后的键盘焦点和页面可交互性。

## 不在本次范围

- 新增总请求数或用户请求维度。
- 修改性能采集器、聚合表、保留期或失败分类。
- 在列表中加入套餐、隐私状态、订阅到期等第二行徽标信息。
- 新增导出、搜索、告警或批量操作。
