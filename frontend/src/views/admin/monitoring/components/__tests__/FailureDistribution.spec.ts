import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FailureDistribution from '../FailureDistribution.vue'

describe('FailureDistribution', () => {
  it('shows empty text when all counts are zero', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [{ label: '限流', count: 0, color: '#f97316' }] } })
    expect(wrapper.text()).toContain('暂无失败记录')
  })

  it('renders provided labels in the accessible summary', () => {
    const wrapper = mount(FailureDistribution, { props: { title: '失败分布', failures: [{ label: '限流', count: 3, color: '#f97316' }] } })
    expect(wrapper.text()).toContain('限流：3')
  })
})
