import { defineComponent, h } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import StaticHtmlFrame from '@/components/common/StaticHtmlFrame.vue'
import type { CustomMenuItem } from '@/types'
import CustomPageView from '../CustomPageView.vue'

enableAutoUnmount(afterEach)

const {
  appStoreState,
  adminMenuItems,
  fetchPublicSettings,
  getHtml,
  fetchPage
} = vi.hoisted(() => ({
  appStoreState: {
    cachedPublicSettings: {
      custom_menu_items: [] as CustomMenuItem[]
    },
    publicSettingsLoaded: true
  },
  adminMenuItems: [] as CustomMenuItem[],
  fetchPublicSettings: vi.fn(),
  getHtml: vi.fn(),
  fetchPage: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'zh-CN' }
    })
  }
})

vi.mock('@/api', () => ({
  pagesAPI: { getHtml }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: appStoreState.cachedPublicSettings,
    publicSettingsLoaded: appStoreState.publicSettingsLoaded,
    fetchPublicSettings
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: false,
    token: 'jwt-token',
    user: { id: 7 }
  })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: adminMenuItems })
}))

const AppLayoutStub = defineComponent({
  name: 'AppLayout',
  setup(_, { slots }) {
    return () => h('main', slots.default?.())
  }
})

function menuItem(overrides: Partial<CustomMenuItem>): CustomMenuItem {
  return {
    id: 'about',
    label: 'About',
    icon_svg: '',
    url: 'html:about',
    content_type: 'html',
    page_slug: 'about',
    visibility: 'user',
    sort_order: 0,
    ...overrides
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function mountView(id: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/custom/:id',
        component: { render: () => h('div') }
      }
    ]
  })
  await router.push(`/custom/${id}`)
  await router.isReady()

  const wrapper = mount(CustomPageView, {
    global: {
      plugins: [router],
      stubs: {
        AppLayout: AppLayoutStub,
        Icon: true
      }
    }
  })
  await flushPromises()
  return { wrapper, router }
}

describe('CustomPageView', () => {
  beforeEach(() => {
    appStoreState.cachedPublicSettings.custom_menu_items = []
    appStoreState.publicSettingsLoaded = true
    adminMenuItems.splice(0)
    fetchPublicSettings.mockReset()
    fetchPublicSettings.mockResolvedValue(undefined)
    getHtml.mockReset()
    fetchPage.mockReset()
    vi.stubGlobal('fetch', fetchPage)
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('fetches HTML and renders it through the shared static frame', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [menuItem({})]
    getHtml.mockResolvedValue('<style>h1{color:red}</style><h1>About</h1>')

    const { wrapper } = await mountView('about')

    expect(getHtml).toHaveBeenCalledWith('about')
    expect(wrapper.getComponent(StaticHtmlFrame).props('source')).toContain(
      '<h1>About</h1>'
    )
    expect(wrapper.find('.custom-embed-frame').exists()).toBe(false)
  })

  it('shows a localized loading state while HTML is requested', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [menuItem({})]
    getHtml.mockReturnValue(new Promise(() => {}))

    const { wrapper } = await mountView('about')

    expect(wrapper.get('[data-testid="html-page-loading"]').text()).toContain(
      'customPage.htmlLoading'
    )
  })

  it('shows an inert failure state without rendering exception text', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [menuItem({})]
    getHtml.mockRejectedValue(new Error('<script>alert(1)</script>'))

    const { wrapper } = await mountView('about')

    expect(wrapper.get('[data-testid="html-page-unavailable"]').text()).toContain(
      'customPage.htmlUnavailable'
    )
    expect(wrapper.html()).not.toContain('alert(1)')
    expect(wrapper.findComponent(StaticHtmlFrame).exists()).toBe(false)
  })

  it('ignores a stale HTML response after navigating to another custom page', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({ id: 'first', label: 'First', url: 'html:first', page_slug: 'first' }),
      menuItem({ id: 'second', label: 'Second', url: 'html:second', page_slug: 'second' })
    ]
    const first = deferred<string>()
    const second = deferred<string>()
    getHtml.mockImplementation((slug: string) => {
      return slug === 'first' ? first.promise : second.promise
    })
    const { wrapper, router } = await mountView('first')

    await router.push('/custom/second')
    await flushPromises()
    second.resolve('<h1>Second</h1>')
    await flushPromises()
    expect(wrapper.getComponent(StaticHtmlFrame).props('source')).toBe(
      '<h1>Second</h1>'
    )

    first.resolve('<h1>First</h1>')
    await flushPromises()
    expect(wrapper.getComponent(StaticHtmlFrame).props('source')).toBe(
      '<h1>Second</h1>'
    )
  })

  it('keeps URL menu items on the existing external iframe path', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        label: 'Docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined
      })
    ]

    const { wrapper } = await mountView('docs')

    expect(wrapper.get('iframe.custom-embed-frame').attributes('src')).toContain(
      'https://example.com/docs'
    )
    expect(wrapper.findComponent(StaticHtmlFrame).exists()).toBe(false)
    expect(getHtml).not.toHaveBeenCalled()
  })

  it('keeps legacy Markdown menu items on the existing Markdown path', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'guide',
        label: 'Guide',
        url: 'md:guide',
        content_type: undefined,
        page_slug: undefined
      })
    ]
    fetchPage.mockResolvedValue({
      ok: true,
      text: async () => '# Guide'
    } as Response)

    const { wrapper } = await mountView('guide')

    expect(fetchPage).toHaveBeenCalledWith(
      expect.stringContaining('/pages/guide'),
      expect.any(Object)
    )
    expect(wrapper.get('.markdown-page-content').text()).toContain('Guide')
    expect(wrapper.findComponent(StaticHtmlFrame).exists()).toBe(false)
    expect(getHtml).not.toHaveBeenCalled()
  })
})
