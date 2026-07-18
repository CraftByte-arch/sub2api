import { apiClient } from '../client'
import type { AccountPlatform, AccountType } from '@/types'

export type PerformanceRange = '1h' | '6h' | '24h' | '7d' | '30d' | '90d'
export type PerformanceOrder = 'asc' | 'desc'

export interface PerformanceRatio {
  numerator: number
  denominator: number
  rate: number
}

export interface PerformanceHealth {
  status: 'complete' | 'degraded' | string
  dropped_samples: number
  pending_samples: number
  last_successful_flush_at: string | null
}

export interface PerformanceLatencyHistogram {
  Samples: number
  LE1000MS: number
  LE2500MS: number
  LE5000MS: number
  LE10000MS: number
  LE30000MS: number
  GT30000MS: number
}

export interface PerformanceCounters {
  attempt_count: number
  success_count: number
  client_canceled_count: number
  ttft_timeout_count: number
  rate_limit_count: number
  auth_count: number
  upstream_4xx_count: number
  upstream_5xx_count: number
  transport_count: number
  protocol_count: number
  other_failure_count: number
  failover_count: number
  ttft_sum_ms: number
  duration_sum_ms: number
  ttft_latency: PerformanceLatencyHistogram
  duration_latency: PerformanceLatencyHistogram
}

export interface PerformanceTimePoint {
  bucket_start: string
  counters: PerformanceCounters
}

export interface PerformanceOverview {
  summary: {
    attempts: number
    availability: PerformanceRatio
    failure_rate: PerformanceRatio
    p50_ttft_ms: number
    p95_ttft_ms: number
    p95_duration_ms: number
    ttft_timeout_count: number
  }
  trend: PerformanceTimePoint[]
  collection_health: PerformanceHealth
  coverage_start: string
  coverage_end: string
}

export interface PerformanceAccountItem {
  account_id: number
  account_name: string
  account_type: AccountType | ''
  auth_mode?: string
  platform: AccountPlatform
  counters: PerformanceCounters
  availability: number
  failure_rate: number
  health_score: number
  p95_ttft_ms: number
  p95_duration_ms: number
  low_sample: boolean
}

export interface PerformanceAccountPage {
  items: PerformanceAccountItem[]
  total: number
  page: number
  page_size: number
  pages: number
  collection_health: PerformanceHealth
}

export interface PerformanceFailureBreakdown {
  Outcome: string
  Count: number
}

export interface PerformanceInvestigation {
  time_points: PerformanceTimePoint[]
  failures: PerformanceFailureBreakdown[]
  collection_health: PerformanceHealth
}

export interface PerformanceParams {
  range?: PerformanceRange
  platform?: string
  group_id?: number
  model?: string
  protocol?: string
  account_id?: number
}

export interface PerformanceMetrics {
  availability: number
  failure_rate: number
  ttft_timeout_rate: number
  failover_rate: number
  p50_ttft_ms: number
  p95_ttft_ms: number
  p95_duration_ms: number
}

const withDefaultRange = <T extends PerformanceParams>(params: T = {} as T) => ({ ...params, range: params.range ?? '24h' })

export function performancePercentile(histogram: PerformanceLatencyHistogram, percentile: number): number {
  if (percentile <= 0 || percentile > 1 || histogram.Samples <= 0) return 0

  const rank = Math.ceil(percentile * histogram.Samples)
  const buckets: Array<[number, number]> = [
    [1000, histogram.LE1000MS],
    [2500, histogram.LE2500MS],
    [5000, histogram.LE5000MS],
    [10000, histogram.LE10000MS],
    [30000, histogram.LE30000MS]
  ]

  for (const [upperBound, cumulative] of buckets) {
    if (rank <= cumulative) return upperBound
  }

  return rank <= histogram.LE30000MS + histogram.GT30000MS ? 30001 : 0
}

export function performanceMetricsFromCounters(counters: PerformanceCounters): PerformanceMetrics {
  const denominator = counters.attempt_count - counters.client_canceled_count
  const rate = (numerator: number, divisor: number) => divisor > 0 ? numerator / divisor : 0

  return {
    availability: rate(counters.success_count, denominator),
    failure_rate: rate(denominator - counters.success_count, denominator),
    ttft_timeout_rate: rate(counters.ttft_timeout_count, denominator),
    failover_rate: rate(counters.failover_count, counters.attempt_count),
    p50_ttft_ms: performancePercentile(counters.ttft_latency, 0.5),
    p95_ttft_ms: performancePercentile(counters.ttft_latency, 0.95),
    p95_duration_ms: performancePercentile(counters.duration_latency, 0.95)
  }
}

export async function getOverview(params: PerformanceParams = {}): Promise<PerformanceOverview> {
  const { data } = await apiClient.get<PerformanceOverview>('/admin/performance/overview', { params: withDefaultRange(params) })
  return data
}

export async function getAccounts(params: PerformanceParams & { sort?: string; order?: PerformanceOrder; page?: number; page_size?: number } = {}): Promise<PerformanceAccountPage> {
  const { data } = await apiClient.get<PerformanceAccountPage>('/admin/performance/accounts', { params: withDefaultRange(params) })
  return data
}

export async function getInvestigation(params: PerformanceParams = {}): Promise<PerformanceInvestigation> {
  const { data } = await apiClient.get<PerformanceInvestigation>('/admin/performance/investigation', { params: withDefaultRange(params) })
  return data
}

export default { getOverview, getAccounts, getInvestigation }
