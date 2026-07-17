import { apiClient } from '../client'

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
  samples: number
  le_1000_ms: number
  le_2500_ms: number
  le_5000_ms: number
  le_10000_ms: number
  le_30000_ms: number
  gt_30000_ms: number
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
  platform: string
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
  outcome: string
  count: number
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
  if (percentile <= 0 || percentile > 1 || histogram.samples <= 0) return 0

  const rank = Math.ceil(percentile * histogram.samples)
  const buckets: Array<[number, number]> = [
    [1000, histogram.le_1000_ms],
    [2500, histogram.le_2500_ms],
    [5000, histogram.le_5000_ms],
    [10000, histogram.le_10000_ms],
    [30000, histogram.le_30000_ms]
  ]

  for (const [upperBound, cumulative] of buckets) {
    if (rank <= cumulative) return upperBound
  }

  return rank <= histogram.le_30000_ms + histogram.gt_30000_ms ? 30001 : 0
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
