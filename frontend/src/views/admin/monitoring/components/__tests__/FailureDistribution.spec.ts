import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? key + ' ' + JSON.stringify(params) : key }) }
})

import FailureDistribution from '../FailureDistribution.vue'

describe('FailureDistribution', () => {
  it('shows empty text when all counts are zero', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [{ label: '限流', count: 0, color: '#f97316' }] } })
    expect(wrapper.text()).toContain('admin.monitoring.failures.empty')
  })

  it('renders provided labels in the accessible summary', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [{ label: '限流', count: 3, color: '#f97316' }] } })
    expect(wrapper.text()).toContain('限流：3')
  })

  it('shows the localized loading text while loading', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [], loading: true } })
    expect(wrapper.text()).toContain('admin.monitoring.failures.loading')
  })
})
