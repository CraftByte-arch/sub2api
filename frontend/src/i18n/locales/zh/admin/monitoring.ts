export default {
  monitoring: {
    title: '监控中心',
    description: '请求健康度、首字节延迟与账号级异常的统一视图',
    coverage: '数据覆盖 {start} 至 {end}',
    health: {
      complete: '采集完整',
      degraded: '采集降级',
      pending: '等待采集'
    },
    degradedBanner: '采集器当前降级，指标可能不完整。已丢弃 {dropped} 个样本，{pending} 个待写入。',
    protection: {
      enabled: '首 Token 保护 · {seconds}s',
      disabled: '首 Token 保护未启用',
      adjust: '调整'
    },
    filters: {
      range: '时间范围',
      platform: '平台',
      allPlatforms: '全部平台',
      model: '模型',
      modelPlaceholder: '按模型过滤'
    },
    kpi: {
      availability: '可用率',
      failureRate: '失败率',
      ttftTimeoutRate: 'TTFT 超时率',
      recoveryRate: '换号恢复率',
      p95Ttft: 'P95 TTFT',
      requestsContext: '{count} 次请求',
      ratioContext: '{numerator} / {denominator}',
      timeoutsContext: '{count} 次超时',
      p95TtftContext: 'P50 {p50} · 总耗时 {duration}'
    },
    funnel: {
      title: '首 Token 保护路径',
      subtitle: '超时触发后的换号恢复与最终结果',
      controlled: '受控请求',
      triggered: '触发超时',
      recovered: '换号恢复',
      finalFailure: '最终失败',
      platformNote: '漏斗数据不随平台筛选变化'
    },
    trends: {
      rates: '请求健康趋势',
      latency: '延迟趋势',
      availability: '可用率',
      failureRate: '失败率',
      ttftTimeoutRate: 'TTFT 超时率',
      p50Ttft: 'P50 TTFT',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 总耗时',
      empty: '所选时间段暂无数据'
    },
    accounts: {
      title: '账号表现',
      total: '{count} 个账号',
      searchPlaceholder: '搜索账号名称或 ID',
      account: '账号',
      platform: '平台',
      status: '状态',
      availability: '可用率',
      failureRate: '失败率',
      ttftTimeoutRate: 'TTFT 超时率',
      p95Ttft: 'P95 TTFT',
      samples: '样本数',
      healthy: '健康',
      watch: '关注',
      risk: '风险',
      lowSample: '样本不足',
      empty: '所选时间段暂无账号性能数据'
    },
    failures: {
      title: '失败分布',
      empty: '暂无失败记录',
      outcomes: {
        ttft_timeout: 'TTFT 超时',
        rate_limit: '限流',
        auth: '鉴权',
        upstream_4xx: '上游 4xx',
        upstream_5xx: '上游 5xx',
        transport: '网络传输',
        protocol: '协议',
        other_failure: '其他失败'
      }
    },
    drawer: {
      title: '账号性能详情',
      loading: '正在加载账号详情',
      empty: '暂无可供分析的性能数据',
      availability: '可用率',
      failureRate: '失败率',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 总耗时',
      successContext: '{success} / {total} 次成功',
      failureContext: '{failure} / {total} 次失败',
      ttftContext: '首字节响应延迟',
      durationContext: '完整请求耗时',
      trendTitle: '性能趋势',
      failureTitle: '失败分布'
    },
    settings: {
      title: '首 Token 超时保护',
      description: '请求超过设定秒数未返回首个 Token 时，自动切换账号重试。',
      enabled: '启用保护',
      timeoutSeconds: '超时时间（秒）',
      timeoutError: '超时时间必须是 1-300 的整数',
      effectiveEnabled: '当前生效：{seconds} 秒',
      effectiveDisabled: '当前未启用'
    },
    empty: {
      title: '暂无可分析样本',
      description: '性能样本会在部署完成并处理请求后逐步累积。'
    },
    errors: {
      overview: '无法加载监控概览',
      accounts: '无法加载账号性能数据',
      investigation: '无法加载账号性能详情',
      settings: '无法保存设置'
    }
  }
}
