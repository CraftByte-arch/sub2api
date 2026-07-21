import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? key + ' ' + JSON.stringify(params) : key }) }
})

import MetricTrendCard from '../MetricTrendCard.vue'

const baseProps = { label: '可用率', value: '99.95%', context: '10,000 次请求', tone: 'success' as const }

describe('MetricTrendCard', () => {
  it('renders value, context and sparkline for a trend', () => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, trend: [0.998, 0.999, 0.9995] } })
    expect(wrapper.text()).toContain('99.95%')
    expect(wrapper.text()).toContain('10,000 次请求')
    const ariaLabel = wrapper.get('[data-testid="metric-trend-sparkline"]').attributes('aria-label') ?? ''
    expect(ariaLabel).toContain('admin.monitoring.kpi.trendAria')
    expect(ariaLabel).toContain('可用率')
  })

  it('renders a localized sr-only trend summary', () => {
    const wrapper = mount(MetricTrendCard, { props: { ...baseProps, trend: [1, 2, 3] } })
    const summary = wrapper.find('p.sr-only')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('admin.monitoring.kpi.trendSummary')
    expect(summary.text()).toContain('admin.monitoring.kpi.trendUp')
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
