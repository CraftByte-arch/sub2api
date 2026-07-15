import { apiClient } from '../client'

export type TTFTRange = '24h' | '7d' | '30d' | '90d'
export type TTFTProtocol = 'responses' | 'chat_completions' | 'anthropic_messages'
export type TTFTOrder = 'asc' | 'desc'

export interface RateMetric {
  numerator: number
  denominator: number
  rate: number
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

export interface TTFTCompleteness {
  status: 'healthy' | 'degraded' | string
  dropped_samples: number
  last_successful_flush_at: string | null
  pending_samples: number
}

export interface TTFTTrendPoint {
  bucket_start: string
  attempt_ttft_timeout_rate: RateMetric
  recovery_rate: RateMetric
  final_ttft_failure_rate: RateMetric
  other_final_failure_rate: RateMetric
}

export interface TTFTOverview {
  summary: {
    controlled_requests: number
    client_canceled_requests: number
    attempt_ttft_timeout_rate: RateMetric
    recovery_rate: RateMetric
    final_ttft_failure_rate: RateMetric
    other_final_failure_rate: RateMetric
  }
  trend: TTFTTrendPoint[]
  other_failures: Array<{ failure_kind: string; sample_count: number }>
  completeness: TTFTCompleteness
}

export interface TTFTOverviewParams {
  range?: TTFTRange
  protocol?: TTFTProtocol
  model?: string
}

export interface TTFTAccountStats {
  account_id: number
  account_name: string
  platform: string
  samples: number
  success_count: number
  ttft_timeout_count: number
  ttft_timeout_rate: RateMetric
  other_failure_count: number
  other_failure_rate: RateMetric
  avg_ttft_ms: number
  low_sample: boolean
}

export interface TTFTAccountsPage {
  items: TTFTAccountStats[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface TTFTAccountsParams extends TTFTOverviewParams {
  platform?: string
  account_id?: number
  search?: string
  sort?: string
  order?: TTFTOrder
  page?: number
  page_size?: 10 | 20 | 50 | 100
}

function withDefaultRange<T extends { range?: TTFTRange }>(params: T = {} as T): T & { range: TTFTRange } {
  return { ...params, range: params.range ?? '24h' }
}

export async function getSettings(): Promise<FirstTokenTimeoutSettings> {
  const { data } = await apiClient.get<FirstTokenTimeoutSettings>('/admin/settings/first-token-timeout')
  return data
}

export async function updateSettings(payload: FirstTokenTimeoutSettingsValue): Promise<FirstTokenTimeoutSettings> {
  const { data } = await apiClient.put<FirstTokenTimeoutSettings>('/admin/settings/first-token-timeout', payload)
  return data
}

export async function getOverview(params: TTFTOverviewParams = {}): Promise<TTFTOverview> {
  const { data } = await apiClient.get<TTFTOverview>('/admin/ttft/overview', { params: withDefaultRange(params) })
  return data
}

export async function getAccounts(params: TTFTAccountsParams = {}): Promise<TTFTAccountsPage> {
  const { data } = await apiClient.get<TTFTAccountsPage>('/admin/ttft/accounts', { params: withDefaultRange(params) })
  return data
}

export default { getSettings, updateSettings, getOverview, getAccounts }
