import { beforeEach, describe, expect, it, vi } from 'vitest'

const get = vi.hoisted(() => vi.fn())
const put = vi.hoisted(() => vi.fn())

vi.mock('@/api/client', () => ({ apiClient: { get, put } }))

import monitoringAPI, { getOverview, getAccounts, getSettings, updateSettings } from '../admin/monitoring'

describe('admin monitoring api', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    get.mockResolvedValue({ data: {} })
    put.mockResolvedValue({ data: {} })
  })

  it('requests the aggregated overview with default range', async () => {
    await getOverview({ platform: 'openai' })
    expect(get).toHaveBeenCalledWith('/admin/monitoring/overview', { params: { range: '24h', platform: 'openai' } })
  })

  it('requests accounts with search and pagination', async () => {
    await getAccounts({ range: '7d', search: 'prod', sort: 'health_score', order: 'asc', page: 2, page_size: 20 })
    expect(get).toHaveBeenCalledWith('/admin/performance/accounts', {
      params: { range: '7d', search: 'prod', sort: 'health_score', order: 'asc', page: 2, page_size: 20 }
    })
  })

  it('loads and saves first token timeout settings', async () => {
    await getSettings()
    expect(get).toHaveBeenCalledWith('/admin/settings/first-token-timeout')
    await updateSettings({ enabled: true, timeout_seconds: 30 })
    expect(put).toHaveBeenCalledWith('/admin/settings/first-token-timeout', { enabled: true, timeout_seconds: 30 })
  })

  it('exposes a default export with all methods', () => {
    expect(Object.keys(monitoringAPI).sort()).toEqual(['getAccounts', 'getInvestigation', 'getOverview', 'getSettings', 'updateSettings'])
  })
})
