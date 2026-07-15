import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-chartjs', () => ({
  Line: {
    name: 'LineChart',
    props: ['data', 'options'],
    template: '<div data-testid="line-chart" />'
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import TTFTFailureTrendChart from '../TTFTFailureTrendChart.vue'

const points = [{
  bucket_start: '2026-07-15T00:00:00Z',
  attempt_ttft_timeout_rate: { numerator: 2, denominator: 10, rate: 0.2 },
  recovery_rate: { numerator: 3, denominator: 4, rate: 0.75 },
  final_ttft_failure_rate: { numerator: 1, denominator: 10, rate: 0.1 },
  other_final_failure_rate: { numerator: 2, denominator: 10, rate: 0.2 }
}]

describe('TTFTFailureTrendChart', () => {
  it('includes rate and numerator/denominator in every tooltip and accessible summary', () => {
    const wrapper = mount(TTFTFailureTrendChart, {
      props: { points },
      global: { mocks: { $t: (key: string) => key } }
    })
    const chart = wrapper.getComponent({ name: 'LineChart' })
    const data = chart.props('data') as { datasets: Array<{ label: string }> }
    const options = chart.props('options') as { plugins: { tooltip: { callbacks: { label: (context: { dataset: { label?: string }; parsed: { y: number | null }; dataIndex: number; datasetIndex: number }) => string } } } }

    expect(data.datasets).toHaveLength(4)
    expect(options.plugins.tooltip.callbacks.label({ dataset: data.datasets[0], parsed: { y: 20 }, dataIndex: 0, datasetIndex: 0 })).toContain('20.0% (2 / 10)')
    expect(options.plugins.tooltip.callbacks.label({ dataset: data.datasets[1], parsed: { y: 75 }, dataIndex: 0, datasetIndex: 1 })).toContain('75.0% (3 / 4)')
    expect(wrapper.get('.sr-only').text()).toContain('2 / 10')
    expect(wrapper.get('.sr-only').text()).toContain('3 / 4')
    expect(wrapper.get('.sr-only').text()).toContain('1 / 10')
  })
})
