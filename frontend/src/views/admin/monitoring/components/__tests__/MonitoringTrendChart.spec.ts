import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en'

const translations = vi.hoisted(() => ({
  'admin.monitoring.trends.loading': 'Loading trend data',
  'admin.monitoring.trends.empty': 'No data for the selected range'
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => translations[key as keyof typeof translations] ?? key
    })
  }
})

vi.mock('chart.js', () => ({
  CategoryScale: {},
  Chart: { register: vi.fn() },
  Filler: {},
  Legend: {},
  LineElement: {},
  LinearScale: {},
  PointElement: {},
  Tooltip: {}
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<div data-testid="line-chart" />' }
}))

import MonitoringTrendChart from '../MonitoringTrendChart.vue'

const baseProps = {
  title: 'Request health trend',
  points: [],
  series: []
}

describe('MonitoringTrendChart', () => {
  it('renders localized loading and empty states', async () => {
    const wrapper = mount(MonitoringTrendChart, {
      props: { ...baseProps, loading: true }
    })

    expect(en.admin.monitoring.trends.loading).toBe(translations['admin.monitoring.trends.loading'])
    expect(wrapper.text()).toContain('Loading trend data')

    await wrapper.setProps({ loading: false })
    expect(en.admin.monitoring.trends.empty).toBe(translations['admin.monitoring.trends.empty'])
    expect(wrapper.text()).toContain('No data for the selected range')
  })
})
