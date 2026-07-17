import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TTFTRecoveryFunnel from '../TTFTRecoveryFunnel.vue'

const summary = {
  controlled_requests: 12,
  client_canceled_requests: 2,
  attempt_ttft_timeout_rate: { numerator: 3, denominator: 10, rate: 0.3 },
  recovery_rate: { numerator: 2, denominator: 3, rate: 2 / 3 },
  final_ttft_failure_rate: { numerator: 1, denominator: 12, rate: 1 / 12 },
  other_final_failure_rate: { numerator: 1, denominator: 12, rate: 1 / 12 }
}

describe('TTFTRecoveryFunnel', () => {
  it('shows all policy recovery stages with their exact counts', () => {
    const wrapper = mount(TTFTRecoveryFunnel, { props: { summary } })

    expect(wrapper.get('[data-testid="ttft-recovery-funnel"]').attributes('aria-label')).toContain('12')
    expect(wrapper.text()).toContain('受控请求')
    expect(wrapper.text()).toContain('触发超时')
    expect(wrapper.text()).toContain('换号恢复')
    expect(wrapper.text()).toContain('最终 TTFT 失败')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('3')
    expect(wrapper.text()).toContain('2')
    expect(wrapper.text()).toContain('1')
  })

  it('does not render a funnel when no requests were controlled', () => {
    const wrapper = mount(TTFTRecoveryFunnel, { props: { summary: { ...summary, controlled_requests: 0 } } })

    expect(wrapper.find('[data-testid="ttft-recovery-funnel"]').exists()).toBe(false)
  })
})
