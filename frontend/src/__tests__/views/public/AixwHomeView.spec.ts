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
    
    // Check for logo text
    expect(wrapper.text()).toContain('A AIXW')
    
    // Check for button text
    expect(wrapper.text()).toContain('Get started ->')

    // Check bottom links
    expect(wrapper.text()).toContain('RELIABLE')
    expect(wrapper.text()).toContain('GLOBAL')
    expect(wrapper.text()).toContain('MINIMAL')
    expect(wrapper.text()).toContain('FAST')
  })
})
