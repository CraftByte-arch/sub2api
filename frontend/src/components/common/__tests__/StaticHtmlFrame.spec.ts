import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import StaticHtmlFrame from '@/components/common/StaticHtmlFrame.vue'
import { STATIC_HTML_SANDBOX } from '@/utils/staticHtml'

describe('StaticHtmlFrame', () => {
  it('binds sanitized source into a restricted sandbox', async () => {
    const wrapper = mount(StaticHtmlFrame, {
      props: { source: '<h1>Hello</h1>', title: 'Preview' }
    })
    const iframe = wrapper.get('iframe')

    expect(iframe.attributes('title')).toBe('Preview')
    expect(iframe.attributes('sandbox')).toBe(STATIC_HTML_SANDBOX)
    expect(iframe.attributes('sandbox')).not.toContain('allow-scripts')
    expect(iframe.attributes('sandbox')).not.toContain('allow-same-origin')
    expect(iframe.attributes('referrerpolicy')).toBe('no-referrer')
    expect(iframe.attributes('srcdoc')).toContain('<h1>Hello</h1>')

    await wrapper.setProps({ source: '<p>Updated</p>' })
    expect(wrapper.get('iframe').attributes('srcdoc')).toContain('<p>Updated</p>')
  })
})
