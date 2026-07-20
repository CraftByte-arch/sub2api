import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetricTrendCard from '../MetricTrendCard.vue'

const baseProps = { label: '可用率', value: '99.95%', context: '10,000 次请求', tone: 'success' as const }

describe('MetricTrendCard', () => {
  it('renders value, context and sparkline for a trend', () => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, trend: [0.998, 0.999, 0.9995] } })
    expect(wrapper.text()).toContain('99.95%')
    expect(wrapper.text()).toContain('10,000 次请求')
    expect(wrapper.get('[data-testid="metric-trend-sparkline"]').attributes('aria-label')).toContain('可用率')
  })

  it.each([{ trend: [] }, { trend: [0.9995] }])('hides sparkline for an incomplete trend', ({ trend }) => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, trend } })
    expect(wrapper.find('[data-testid="metric-trend-sparkline"]').exists()).toBe(false)
  })

  it('applies the tone class to the sparkline stroke', () => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, tone: 'danger', trend: [1, 2, 3] } })
    expect(wrapper.get('[data-testid="metric-trend-sparkline"] polyline').classes().join(' ')).toContain('stroke-red-500')
  })
})
