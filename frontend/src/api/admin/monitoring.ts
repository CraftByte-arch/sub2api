import { apiClient } from '../client'
import {
  performanceMetricsFromCounters,
  performanceMetricsFromTimePoint,
  type PerformanceAccountItem,
  type PerformanceAccountPage,
  type PerformanceCounters,
  type PerformanceInvestigation,
  type PerformanceMetrics,
  type PerformanceOrder,
  type PerformanceOverview,
  type PerformanceRange,
  type PerformanceTimePoint
} from './performance'

// ---- Types (mirrors of the removed ttft client, kept self-contained) ----

export type MonitoringRange = PerformanceRange
export type { PerformanceOrder, PerformanceRange }

export interface RateMetric {
  numerator: number
  denominator: number
  rate: number
}

export interface MonitoringTTFTSummary {
  controlled_requests: number
  client_canceled_requests: number
  attempt_ttft_timeout_rate: RateMetric
  recovery_rate: RateMetric
  final_ttft_failure_rate: RateMetric
  other_final_failure_rate: RateMetric
}

export interface TTFTTrendPoint {
  bucket_start: string
  attempt_ttft_timeout_rate: RateMetric
  recovery_rate: RateMetric
  final_ttft_failure_rate: RateMetric
  other_final_failure_rate: RateMetric
}

export interface TTFTFailureDistributionItem {
  failure_kind: string
  sample_count: number
}

export interface CollectionHealth {
  status: 'complete' | 'degraded' | string
  dropped_samples: number
  pending_samples: number
  last_successful_flush_at: string | null
}

export interface MonitoringTTFTOverview {
  summary: MonitoringTTFTSummary
  trend: TTFTTrendPoint[]
  other_failures: TTFTFailureDistributionItem[]
  completeness: CollectionHealth
}

export interface MonitoringOverview {
  performance: PerformanceOverview
  ttft: MonitoringTTFTOverview
}

export interface FirstTokenTimeoutSettingsValue {
  enabled: boolean
  timeout_seconds: number
}

export interface FirstTokenTimeoutSettings {
  saved: FirstTokenTimeoutSettingsValue
  effective: FirstTokenTimeoutSettingsValue
  loaded_at: string
}

export interface MonitoringOverviewParams {
  range?: MonitoringRange
  platform?: string
  model?: string
}

export interface MonitoringAccountsParams extends MonitoringOverviewParams {
  search?: string
  sort?: string
  order?: PerformanceOrder
  page?: number
  page_size?: number
}

export interface MonitoringInvestigationParams extends MonitoringOverviewParams {
  account_id: number
}

function withDefaultRange<T extends { range?: MonitoringRange }>(params: T) {
  return { ...params, range: params.range ?? '24h' }
}

// ---- API ----

export async function getOverview(params: MonitoringOverviewParams): Promise<MonitoringOverview> {
  const { data } = await apiClient.get<MonitoringOverview>('/admin/monitoring/overview', { params: withDefaultRange(params) })
  return data
}

export async function getAccounts(params: MonitoringAccountsParams): Promise<PerformanceAccountPage> {
  const { data } = await apiClient.get<PerformanceAccountPage>('/admin/performance/accounts', { params: withDefaultRange(params) })
  return data
}

export async function getInvestigation(params: MonitoringInvestigationParams): Promise<PerformanceInvestigation> {
  const { data } = await apiClient.get<PerformanceInvestigation>('/admin/performance/investigation', { params: withDefaultRange(params) })
  return data
}

export async function getSettings(): Promise<FirstTokenTimeoutSettings> {
  const { data } = await apiClient.get<FirstTokenTimeoutSettings>('/admin/settings/first-token-timeout')
  return data
}

export async function updateSettings(payload: FirstTokenTimeoutSettingsValue): Promise<FirstTokenTimeoutSettings> {
  const { data } = await apiClient.put<FirstTokenTimeoutSettings>('/admin/settings/first-token-timeout', payload)
  return data
}

export { performanceMetricsFromCounters, performanceMetricsFromTimePoint }
export type { PerformanceAccountItem, PerformanceAccountPage, PerformanceCounters, PerformanceInvestigation, PerformanceMetrics, PerformanceOverview, PerformanceTimePoint }

export default { getOverview, getAccounts, getInvestigation, getSettings, updateSettings }
