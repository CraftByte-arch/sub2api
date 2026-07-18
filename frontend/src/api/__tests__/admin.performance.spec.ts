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

  it('preserves the backend failure breakdown field names', async () => {
    get.mockResolvedValueOnce({
      data: {
        time_points: [],
        failures: [{ Outcome: 'transport', Count: 4 }],
        collection_health: { status: 'complete', dropped_samples: 0, pending_samples: 0, last_successful_flush_at: null }
      }
    })

    const investigation = await performanceAPI.getInvestigation()

    expect(investigation.failures[0]).toEqual({ Outcome: 'transport', Count: 4 })
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
        Samples: 10,
        LE1000MS: 3,
        LE2500MS: 5,
        LE5000MS: 8,
        LE10000MS: 9,
        LE30000MS: 9,
        GT30000MS: 1
      },
      duration_latency: {
        Samples: 10,
        LE1000MS: 1,
        LE2500MS: 2,
        LE5000MS: 4,
        LE10000MS: 7,
        LE30000MS: 9,
        GT30000MS: 1
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
      ttft_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 },
      duration_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 }
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

  it('prefers request-level percentile fields when the aggregate bucket is open ended', () => {
    const counters = {
      attempt_count: 10,
      success_count: 10,
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
      ttft_latency: { Samples: 10, LE1000MS: 0, LE2500MS: 0, LE5000MS: 9, LE10000MS: 9, LE30000MS: 9, GT30000MS: 1 },
      duration_latency: { Samples: 10, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 8, LE30000MS: 9, GT30000MS: 1 }
    }

    expect(performanceMetricsFromCounters(counters, {
      p50_ttft_ms: 3516,
      p95_ttft_ms: 31454,
      p95_duration_ms: 62768
    })).toMatchObject({ p50_ttft_ms: 3516, p95_ttft_ms: 31454, p95_duration_ms: 62768 })
  })
})
