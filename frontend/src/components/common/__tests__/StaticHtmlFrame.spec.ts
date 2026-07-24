import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'

import StaticHtmlFrame from '@/components/common/StaticHtmlFrame.vue'
import { STATIC_HTML_SANDBOX } from '@/utils/staticHtml'

describe('StaticHtmlFrame', () => {
  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

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

  it('rebuilds the sandbox document when the host theme changes', async () => {
    document.documentElement.classList.remove('dark')
    const wrapper = mount(StaticHtmlFrame, {
      props: { source: '<h1>Hello</h1>', title: 'Preview' }
    })

    try {
      expect(wrapper.get('iframe').attributes('srcdoc')).toContain('data-theme="light"')

      document.documentElement.classList.add('dark')
      await Promise.resolve()
      await nextTick()

      expect(wrapper.get('iframe').attributes('srcdoc')).toContain('data-theme="dark"')
    } finally {
      wrapper.unmount()
    }
  })
})
