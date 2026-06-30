import { apiClient } from './client'
import type { UsageCardPlan } from '@/types/payment'

export interface UserUsageCard {
  id: number
  user_id: number
  user?: {
    id: number
    email: string
    username?: string
  }
  plan_id?: number
  name: string
  starts_at: string
  expires_at: string
  total_limit_usd: number
  used_usd: number
  remaining_usd: number
  status: string
  source: string
  notes?: string
  created_at: string
  updated_at: string
}

export interface UsageCardSummary {
  available_count: number
  available_remaining_usd: number
}

export const usageCardsAPI = {
  listMine() {
    return apiClient.get<UserUsageCard[]>('/usage-cards')
  },
  getSummary() {
    return apiClient.get<UsageCardSummary>('/usage-cards/summary')
  },
}

export const adminUsageCardsAPI = {
  listPlans() {
    return apiClient.get<UsageCardPlan[]>('/admin/usage-card-plans')
  },
  createPlan(data: Partial<UsageCardPlan>) {
    return apiClient.post<UsageCardPlan>('/admin/usage-card-plans', data)
  },
  updatePlan(id: number, data: Partial<UsageCardPlan>) {
    return apiClient.put<UsageCardPlan>(`/admin/usage-card-plans/${id}`, data)
  },
  deletePlan(id: number) {
    return apiClient.delete(`/admin/usage-card-plans/${id}`)
  },
  listCards(params?: { user_id?: number; status?: string }) {
    return apiClient.get<UserUsageCard[]>('/admin/usage-cards', { params })
  },
  cancelCard(id: number, reason?: string) {
    return apiClient.post(`/admin/usage-cards/${id}/cancel`, { reason })
  },
  suspendCard(id: number, reason?: string) {
    return apiClient.post(`/admin/usage-cards/${id}/suspend`, { reason })
  },
  resumeCard(id: number, reason?: string) {
    return apiClient.post(`/admin/usage-cards/${id}/resume`, { reason })
  },
}
