import { apiClient } from '../client'

export interface BalanceOverview {
  total_available_balance: number
  total_usage_card_available_balance: number
  usage_card_latest_expires_at: string | null
  generated_at: string
}

export async function getBalanceOverview(): Promise<BalanceOverview> {
  const { data } = await apiClient.get<BalanceOverview>('/admin/balance-overview')
  return data
}
