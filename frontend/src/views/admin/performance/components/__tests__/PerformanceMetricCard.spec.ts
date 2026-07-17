import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PerformanceMetricCard from '../PerformanceMetricCard.vue'

const baseProps = {
  label: '可用率',
  value: '99.95%',
  context: '9,995 / 10,000 次请求',
  tone: 'success' as const,
  icon: 'chart' as const
}

describe('PerformanceMetricCard', () => {
  it('renders the primary value, context, and labelled sparkline for a trend', () => {
    const wrapper = mount(PerformanceMetricCard, { props: { ...baseProps, trend: [98.8, 99.2, 99.95] } })

    expect(wrapper.text()).toContain('99.95%')
    expect(wrapper.text()).toContain('9,995 / 10,000 次请求')
    expect(wrapper.get('[data-testid="performance-sparkline"]').attributes('aria-label')).toContain('可用率')
  })

  it.each([{ trend: [] }, { trend: [99.95] }])('does not render a sparkline for an incomplete trend', ({ trend }) => {
    const wrapper = mount(PerformanceMetricCard, { props: { ...baseProps, trend } })

    expect(wrapper.find('[data-testid="performance-sparkline"]').exists()).toBe(false)
  })
})
