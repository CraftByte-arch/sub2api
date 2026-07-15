import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

vi.mock('vue-chartjs', () => ({ Bar: { name: 'BarChart', props: ['data', 'options'], template: '<div />' } }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import TTFTFailureDistributionChart from '../TTFTFailureDistributionChart.vue'

describe('TTFTFailureDistributionChart', () => {
  it('provides a screen-reader summary of every failure kind and count', () => {
    const wrapper = mount(TTFTFailureDistributionChart, {
      props: { failures: [{ failure_kind: 'transport', sample_count: 3 }, { failure_kind: 'auth', sample_count: 1 }] },
      global: { mocks: { $t: (key: string) => key } }
    })

    expect(wrapper.get('.sr-only').text()).toContain('transport: 3')
    expect(wrapper.get('.sr-only').text()).toContain('auth: 1')
  })

  it('recomputes bar chart tick and grid colors when the document theme changes', async () => {
    document.documentElement.classList.remove('dark')
    const wrapper = mount(TTFTFailureDistributionChart, { props: { failures: [{ failure_kind: 'transport', sample_count: 3 }] }, global: { mocks: { $t: (key: string) => key } } })
    const chart = wrapper.getComponent({ name: 'BarChart' })
    const before = (chart.props('options') as { scales: { x: { ticks: { color: string }; grid: { color: string } } } }).scales.x

    document.documentElement.classList.add('dark')
    await nextTick()
    const after = (chart.props('options') as { scales: { x: { ticks: { color: string }; grid: { color: string } } } }).scales.x

    expect(before.ticks.color).not.toBe(after.ticks.color)
    expect(before.grid.color).not.toBe(after.grid.color)
    document.documentElement.classList.remove('dark')
  })
})
