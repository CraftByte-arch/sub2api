import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-chartjs', () => ({ Bar: { template: '<div />' } }))
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
})
