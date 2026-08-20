## Context

侧车 `/api/overview` 已经读取所有账号的管理员余额投影，前端也用同一投影展示单账号余额。分组视图由账号的真实/逻辑分组成员关系构造，因此可以在现有 overview 请求中一次性计算每个分组的余额摘要。

## Decisions

1. **服务端计算摘要，前端只负责显示。**
   - 后端返回 `group_balance_summaries`，键为分组 ID 字符串。
   - 每个摘要分别记录 `enabled`、`disabled` 两类余额，避免前端重复实现成员归属和状态口径。

2. **可用余额只汇总有限数值。**
   - 账号 `admin_balance.remaining` 为有限非负值时加入对应类别的 `remaining` 和 `account_count`（沿用现有管理员余额投影口径）。
   - `unlimited`、`insufficient`、未同步等状态不参与金额相加，但分别计数，前端显示状态提示。
   - 余额不足状态单独计数；未知余额绝不按零处理。

3. **启用口径复用现有调度状态。**
   - 账号属于某分组后，使用与页面现有 `groupAccountState` 相同的有效调度状态；`enabled` 仅表示 `key === "enabled"`。
   - 保护倍率导致的 `protected`、管理员停止、自动停止、临时受限和停用账号都归入 `disabled`。

4. **兼容和响应式。**
   - 缺少 `group_balance_summaries` 时前端回退为不显示摘要，不影响旧侧车状态文件或旧 overview 数据。
   - 桌面端在分组统计区显示两项紧凑指标，小屏自动换行；长提示通过 `title` 和可见文字表达。

## Data Shape

```json
{
  "group_balance_summaries": {
    "7": {
      "enabled": {"remaining": 12.5, "account_count": 2, "unavailable_count": 0, "unlimited_count": 0, "insufficient_count": 0},
      "disabled": {"remaining": -0.3, "account_count": 1, "unavailable_count": 1, "unlimited_count": 0, "insufficient_count": 0}
    }
  }
}
```

## Risks / Trade-offs

- [余额类别与页面状态漂移] → 在后端增加与前端状态语义一致的轻量状态分类测试；前端仍使用摘要提供的文字计数。
- [多分组账号重复汇总] → 账号按其每个成员分组分别计入，这是分组视图应有的语义，不跨组合并。
- [无限额度误显示为金额] → 无限额度只计数并显示“含不限额度”，不参与金额总和。
