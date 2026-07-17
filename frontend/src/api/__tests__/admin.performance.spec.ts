import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get }
}))

import performanceAPI, { performanceMetricsFromCounters } from '../admin/performance'

describe('admin performance API', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: {} })
  })

  it('requests an account investigation with the supplied filter', async () => {
    await performanceAPI.getInvestigation({ range: '24h', account_id: 42 })

    expect(get).toHaveBeenCalledWith('/admin/performance/investigation', {
      params: { range: '24h', account_id: 42 }
    })
  })

  it('derives outcome ratios and latency percentiles from cumulative counters', () => {
    const metrics = performanceMetricsFromCounters({
      attempt_count: 20,
      success_count: 12,
      client_canceled_count: 2,
      ttft_timeout_count: 3,
      rate_limit_count: 1,
      auth_count: 0,
      upstream_4xx_count: 0,
      upstream_5xx_count: 1,
      transport_count: 1,
      protocol_count: 0,
      other_failure_count: 0,
      failover_count: 5,
      ttft_sum_ms: 0,
      duration_sum_ms: 0,
      ttft_latency: {
        samples: 10,
        le_1000_ms: 3,
        le_2500_ms: 5,
        le_5000_ms: 8,
        le_10000_ms: 9,
        le_30000_ms: 9,
        gt_30000_ms: 1
      },
      duration_latency: {
        samples: 10,
        le_1000_ms: 1,
        le_2500_ms: 2,
        le_5000_ms: 4,
        le_10000_ms: 7,
        le_30000_ms: 9,
        gt_30000_ms: 1
      }
    })

    expect(metrics).toEqual({
      availability: 12 / 18,
      failure_rate: 6 / 18,
      ttft_timeout_rate: 3 / 18,
      failover_rate: 5 / 20,
      p50_ttft_ms: 2500,
      p95_ttft_ms: 30001,
      p95_duration_ms: 30001
    })
  })

  it('returns zero-valued metrics for empty denominators and histograms', () => {
    expect(performanceMetricsFromCounters({
      attempt_count: 0,
      success_count: 0,
      client_canceled_count: 0,
      ttft_timeout_count: 0,
      rate_limit_count: 0,
      auth_count: 0,
      upstream_4xx_count: 0,
      upstream_5xx_count: 0,
      transport_count: 0,
      protocol_count: 0,
      other_failure_count: 0,
      failover_count: 0,
      ttft_sum_ms: 0,
      duration_sum_ms: 0,
      ttft_latency: { samples: 0, le_1000_ms: 0, le_2500_ms: 0, le_5000_ms: 0, le_10000_ms: 0, le_30000_ms: 0, gt_30000_ms: 0 },
      duration_latency: { samples: 0, le_1000_ms: 0, le_2500_ms: 0, le_5000_ms: 0, le_10000_ms: 0, le_30000_ms: 0, gt_30000_ms: 0 }
    })).toEqual({
      availability: 0,
      failure_rate: 0,
      ttft_timeout_rate: 0,
      failover_rate: 0,
      p50_ttft_ms: 0,
      p95_ttft_ms: 0,
      p95_duration_ms: 0
    })
  })
})
