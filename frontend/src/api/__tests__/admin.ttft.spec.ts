import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get, put }
}))

import ttftAPI from '../admin/ttft'

describe('admin TTFT API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    get.mockResolvedValue({ data: { data: {} } })
    put.mockResolvedValue({ data: { data: {} } })
  })

  it('uses the default 24h overview query and preserves global filters', async () => {
    await ttftAPI.getOverview({ protocol: 'responses', model: 'gpt-5' })

    expect(get).toHaveBeenCalledWith('/admin/ttft/overview', {
      params: { range: '24h', protocol: 'responses', model: 'gpt-5' }
    })
  })

  it('maps account-local filters only to the accounts request', async () => {
    await ttftAPI.getAccounts({
      range: '7d',
      protocol: 'chat_completions',
      model: 'gpt-5-mini',
      platform: 'openai',
      account_id: 42,
      search: 'prod',
      sort: 'ttft_timeout_rate',
      order: 'asc',
      page: 2,
      page_size: 50
    })

    expect(get).toHaveBeenCalledWith('/admin/ttft/accounts', {
      params: {
        range: '7d',
        protocol: 'chat_completions',
        model: 'gpt-5-mini',
        platform: 'openai',
        account_id: 42,
        search: 'prod',
        sort: 'ttft_timeout_rate',
        order: 'asc',
        page: 2,
        page_size: 50
      }
    })
  })

  it('reads and updates the isolated timeout settings endpoint', async () => {
    await ttftAPI.getSettings()
    await ttftAPI.updateSettings({ enabled: true, timeout_seconds: 30 })

    expect(get).toHaveBeenCalledWith('/admin/settings/first-token-timeout')
    expect(put).toHaveBeenCalledWith('/admin/settings/first-token-timeout', {
      enabled: true,
      timeout_seconds: 30
    })
  })
})
