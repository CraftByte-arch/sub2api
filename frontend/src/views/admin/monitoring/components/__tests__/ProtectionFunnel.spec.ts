import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? key + ' ' + JSON.stringify(params) : key }) }
})

import ProtectionFunnel from '../ProtectionFunnel.vue'

const summary = {
  controlled_requests: 1000,
  client_canceled_requests: 10,
  attempt_ttft_timeout_rate: { numerator: 100, denominator: 1000, rate: 0.1 },
  recovery_rate: { numerator: 80, denominator: 100, rate: 0.8 },
  final_ttft_failure_rate: { numerator: 20, denominator: 100, rate: 0.2 },
  other_final_failure_rate: { numerator: 0, denominator: 100, rate: 0 }
}

describe('ProtectionFunnel', () => {
  it('renders four stages with conversion rates', () => {
    const wrapper = mount(ProtectionFunnel, { props: { summary } })
    expect(wrapper.text()).toContain('1,000')
    expect(wrapper.text()).toContain('100')
    expect(wrapper.text()).toContain('80')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('10.0%') // 触发超时占比
    expect(wrapper.text()).toContain('80.0%') // 恢复率
    expect(wrapper.text()).toContain('20.0%') // 最终失败率
  })

  it('renders nothing without controlled requests', () => {
    const wrapper = mount(ProtectionFunnel, { props: { summary: { ...summary, controlled_requests: 0 } } })
    expect(wrapper.find('[data-testid="protection-funnel"]').exists()).toBe(false)
  })

  it('renders the platform note under the subtitle', () => {
    const wrapper = mount(ProtectionFunnel, { props: { summary } })
    expect(wrapper.text()).toContain('admin.monitoring.funnel.platformNote')
  })
})
