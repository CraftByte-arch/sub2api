import { describe, it, expect, vi } from 'vitest'
import { mount, RouterLinkStub } from '@vue/test-utils'
import AixwHomeView from '@/views/public/AixwHomeView.vue'
import { createRouter, createWebHistory } from 'vue-router'

describe('AixwHomeView', () => {
  it('renders the main content and button', () => {
    const wrapper = mount(AixwHomeView, {
      global: {
        stubs: {
          RouterLink: RouterLinkStub
        }
      }
    })

    // Check for main text
    expect(wrapper.text()).toContain('Move faster.')
    
    // Check for logo image
    const logo = wrapper.find('img[alt="AIXW Logo"]')
    expect(logo.exists()).toBe(true)
    expect(logo.attributes('src')).toContain('aixw-logo-alpha')

    const page = wrapper.get('[data-testid="aixw-home-page"]')
    expect(page.attributes('style')).toContain('hero-background')
    
    // Check for button text
    expect(wrapper.text()).toContain('Get started')

    // Check bottom links
    expect(wrapper.text()).toContain('RELIABLE')
    expect(wrapper.text()).toContain('GLOBAL')
    expect(wrapper.text()).toContain('MINIMAL')
    expect(wrapper.text()).toContain('FAST')
  })
})
