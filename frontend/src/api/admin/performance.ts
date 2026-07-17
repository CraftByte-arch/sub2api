import { apiClient } from '../client'

export type PerformanceRange = '1h' | '6h' | '24h' | '7d' | '30d' | '90d'
export type PerformanceOrder = 'asc' | 'desc'

export interface PerformanceRatio { numerator: number; denominator: number; rate: number }
export interface PerformanceHealth { status: 'complete' | 'degraded' | string; dropped_samples: number; pending_samples: number; last_successful_flush_at: string | null }
export interface PerformanceOverview {
  summary: { attempts: number; availability: PerformanceRatio; failure_rate: PerformanceRatio; p50_ttft_ms: number; p95_ttft_ms: number; p95_duration_ms: number; ttft_timeout_count: number }
  trend: Array<{ bucket_start: string; Counters?: Record<string, number> }>
  collection_health: PerformanceHealth
  coverage_start: string
  coverage_end: string
}
export interface PerformanceAccountPage { items: Array<{ account_id: number; platform: string; availability: number; failure_rate: number; health_score: number; p95_ttft_ms: number; p95_duration_ms: number; low_sample: boolean }>; total: number; page: number; page_size: number; pages: number; collection_health: PerformanceHealth }
export interface PerformanceParams { range?: PerformanceRange; platform?: string; group_id?: number; model?: string; protocol?: string; account_id?: number }
const withDefaultRange = <T extends PerformanceParams>(params: T = {} as T) => ({ ...params, range: params.range ?? '24h' })

export async function getOverview(params: PerformanceParams = {}): Promise<PerformanceOverview> { const { data } = await apiClient.get('/admin/performance/overview', { params: withDefaultRange(params) }); return data }
export async function getAccounts(params: PerformanceParams & { sort?: string; order?: PerformanceOrder; page?: number; page_size?: number } = {}): Promise<PerformanceAccountPage> { const { data } = await apiClient.get('/admin/performance/accounts', { params: withDefaultRange(params) }); return data }
export default { getOverview, getAccounts }
