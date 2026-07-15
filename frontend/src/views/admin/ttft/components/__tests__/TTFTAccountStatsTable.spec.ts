import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TTFTAccountStatsTable from '../TTFTAccountStatsTable.vue'

const page = {
  items: [{ account_id: 42, account_name: 'production', platform: 'openai', samples: 20, success_count: 17, ttft_timeout_count: 2, ttft_timeout_rate: { numerator: 2, denominator: 20, rate: 0.1 }, other_failure_count: 1, other_failure_rate: { numerator: 1, denominator: 20, rate: 0.05 }, avg_ttft_ms: 123, low_sample: false }],
  total: 1, page: 1, page_size: 20, pages: 1
}

describe('TTFTAccountStatsTable', () => {
  it('keeps loaded rows visible and offers retry after a refresh failure', async () => {
    const wrapper = mount(TTFTAccountStatsTable, { props: { page, loading: false, error: '', sort: 'samples', order: 'desc' }, global: { mocks: { $t: (key: string) => key } } })
    await wrapper.setProps({ error: 'admin.ttft.errors.accounts' })

    expect(wrapper.text()).toContain('production')
    expect(wrapper.text()).toContain('admin.ttft.errors.accounts')
    await wrapper.get('[data-testid="ttft-accounts-retry"]').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })
})
