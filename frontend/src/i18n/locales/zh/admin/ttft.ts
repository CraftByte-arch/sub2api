export default {
  ttft: {
    title: '首 Token 监控',
    description: '监控首 Token 超时切换与账号级结果。',
    settings: {
      title: '首 Token 超时策略',
      description: '保存后仅对新发起的合格流式请求生效。',
      enabled: '启用首 Token 超时',
      timeoutSeconds: '超时秒数',
      timeoutError: '请输入 1 到 300 之间的整数。',
      effectiveEnabled: '当前生效：已启用（{seconds} 秒）',
      effectiveDisabled: '当前生效：已关闭',
      loadedAt: '加载于 {time}'
    },
    filters: { range: '时间范围', protocol: '协议', model: '模型' },
    metrics: {
      controlledRequests: '受控请求数',
      clientCanceled: '客户端取消 {count}',
      attemptTimeout: 'Attempt TTFT 超时率',
      recovery: '换号恢复率',
      finalTTFTFailure: '最终 TTFT 失败率',
      otherFinalFailure: '其他最终失败率'
    },
    charts: { failureTrend: '每小时失败率', failureDistribution: '其他失败分布', count: '失败数' },
    completeness: { degraded: '统计数据不完整：已丢弃 {dropped} 个样本，待处理 {pending} 个样本。', lastSuccessfulFlush: '最近一次成功写入：{time}。', noSuccessfulFlush: '尚未完成过成功写入。' },
    accounts: {
      title: '账号统计', account: '账号 / 平台', accountId: '账号 ID', accountIdError: '请输入正整数账号 ID。', platform: '平台', pageSize: '每页数量',
      samples: '非取消 Attempt 样本', success: '成功', ttft: 'TTFT 超时', other: '其他失败', avgTTFT: '平均 TTFT',
      lowSample: '低样本（<20）', previous: '上一页', next: '下一页'
    },
    errors: { settings: '无法加载或保存首 Token 超时策略。', overview: '无法加载首 Token 统计。', accounts: '无法加载账号统计。' },
    empty: '当前筛选条件下暂无统计数据。'
  }
}
