import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAppStore } from '@/stores'
import HomeVariantView from '@/views/public/HomeVariantView.vue'
import type { PublicSettings } from '@/types'

vi.mock('@/views/HomeView.vue', () => ({
  default: {
    name: 'HomeView',
    template: '<div data-testid="default-home-stub" />',
  },
}))

vi.mock('@/views/public/AixwHomeView.vue', () => ({
  default: {
    name: 'AixwHomeView',
    template: '<div data-testid="aixw-home-stub" />',
  },
}))

function mountWithHomepageVariant(homepageVariant?: string) {
  setActivePinia(createPinia())
  const appStore = useAppStore()
  if (homepageVariant !== undefined) {
    appStore.cachedPublicSettings = {
      homepage_variant: homepageVariant,
    } as PublicSettings
  }

  return mount(HomeVariantView)
}

describe('HomeVariantView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    delete window.__APP_CONFIG__
  })

  it('renders the default homepage when settings are missing', () => {
    const wrapper = mountWithHomepageVariant()

    expect(wrapper.find('[data-testid="default-home-stub"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="aixw-home-stub"]').exists()).toBe(false)
  })

  it('renders the default homepage for the default variant', () => {
    const wrapper = mountWithHomepageVariant('default')

    expect(wrapper.find('[data-testid="default-home-stub"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="aixw-home-stub"]').exists()).toBe(false)
  })

  it('renders the AIXW homepage for the aixw variant', () => {
    const wrapper = mountWithHomepageVariant('aixw')

    expect(wrapper.find('[data-testid="aixw-home-stub"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="default-home-stub"]').exists()).toBe(false)
  })

  it('uses the injected config before the store is initialized', () => {
    setActivePinia(createPinia())
    window.__APP_CONFIG__ = {
      homepage_variant: 'aixw',
    } as PublicSettings

    const wrapper = mount(HomeVariantView)

    expect(wrapper.find('[data-testid="aixw-home-stub"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="default-home-stub"]').exists()).toBe(false)
  })
})
