import { defineComponent, h, nextTick } from 'vue'
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

let notifyResize: () => void

async function mountEmbed() {
  const { wrapper } = await mountView('docs')
  const shell = wrapper.get('.custom-embed-shell').element
  const button = wrapper.get<HTMLAnchorElement>('.custom-open-fab').element
  const size = { width: 800, height: 600 }
  let capturedPointer: number | null = null
  Object.defineProperties(shell, {
    clientWidth: { get: () => size.width },
    clientHeight: { get: () => size.height }
  })
  Object.defineProperties(button, {
    offsetWidth: { value: 100 },
    offsetHeight: { value: 32 },
    offsetLeft: { get: () => Number.parseFloat(button.style.left || '688') },
    offsetTop: { get: () => Number.parseFloat(button.style.top || '12') },
    setPointerCapture: {
      value: vi.fn((id: number) => {
        capturedPointer = id
      })
    },
    hasPointerCapture: { value: (id: number) => capturedPointer === id },
    releasePointerCapture: {
      value: vi.fn(() => {
        capturedPointer = null
      })
    }
  })
  return { wrapper, button, size }
}

async function pointer(
  button: HTMLElement,
  type: string,
  x: number,
  y: number,
  extra = {}
) {
  const event = new MouseEvent(type, {
    clientX: x,
    clientY: y,
    bubbles: true,
    cancelable: true,
    ...extra
  })
  Object.defineProperties(event, {
    pointerId: { value: 1 },
    isPrimary: { value: true }
  })
  button.dispatchEvent(event)
  await nextTick()
}

function click(button: HTMLElement, detail = 1) {
  const event = new MouseEvent('click', {
    bubbles: true,
    cancelable: true,
    detail
  })
  button.dispatchEvent(event)
  return event
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
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(callback: () => void) {
          notifyResize = callback
        }
        observe() {}
        disconnect() {}
      }
    )
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

  it.each([undefined, false, true])('honors the per-menu hide button setting %s while keeping the iframe', async (hidden) => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        label: 'Docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined,
        hide_open_button: hidden
      })
    ]
    const { wrapper } = await mountView('docs')
    expect(wrapper.find('.custom-open-fab').exists()).toBe(hidden !== true)
    expect(wrapper.get('iframe').attributes('src')).toContain('https://example.com/docs')
  })

  it('preserves the embedded URL, secure link attributes, and normal clicks with small pointer movements', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        label: 'Docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined
      })
    ]
    const { wrapper, button } = await mountEmbed()
    expect(button.href).toBe(wrapper.get('iframe').attributes('src'))
    expect(button.href).toContain('user_id=7')
    expect(button.href).toContain('token=jwt-token')
    expect(button.target).toBe('_blank')
    expect(button.rel).toBe('noopener noreferrer')
    await pointer(button, 'pointerdown', 700, 24)
    await pointer(button, 'pointermove', 702, 25)
    await pointer(button, 'pointerup', 702, 25)
    expect(button.style.left).toBe('')
    expect(click(button).defaultPrevented).toBe(false)
    expect(click(button, 0).defaultPrevented).toBe(false)
  })

  it('captures the pointer across iframe content and suppresses only the click following a drag', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined
      })
    ]
    const { button } = await mountEmbed()
    await pointer(button, 'pointerdown', 700, 24)
    expect(button.setPointerCapture).toHaveBeenCalledWith(1)
    await pointer(button, 'pointermove', 200, 124)
    expect(button.style.left).toBe('188px')
    expect(button.style.top).toBe('112px')
    await pointer(button, 'pointerup', 200, 124)
    expect(button.releasePointerCapture).toHaveBeenCalledWith(1)
    expect(click(button).defaultPrevented).toBe(true)
    expect(click(button).defaultPrevented).toBe(false)
    await pointer(button, 'pointerdown', 200, 124)
    await pointer(button, 'pointermove', 220, 124)
    await pointer(button, 'pointerup', 220, 124)
    expect(click(button, 0).defaultPrevented).toBe(false)
  })

  it('keeps the button inside each boundary and reachable when the container shrinks', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined
      })
    ]
    const { button, size } = await mountEmbed()
    await pointer(button, 'pointerdown', 700, 24)
    await pointer(button, 'pointermove', -1000, -1000)
    expect([button.style.left, button.style.top]).toEqual(['0px', '0px'])
    await pointer(button, 'pointermove', 2000, 2000)
    expect([button.style.left, button.style.top]).toEqual(['700px', '568px'])
    await pointer(button, 'pointerup', 2000, 2000)
    size.width = 300
    size.height = 200
    notifyResize()
    await nextTick()
    expect([button.style.left, button.style.top]).toEqual(['200px', '168px'])
  })

  it('stops moving on cancellation or lost pointer capture and permits the next normal click', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined
      })
    ]
    const { button } = await mountEmbed()
    for (const endEvent of ['pointercancel', 'lostpointercapture']) {
      await pointer(button, 'pointerdown', 700, 24)
      await pointer(button, 'pointermove', 500, 124)
      await pointer(button, endEvent, 500, 124)
      const position = button.style.cssText
      await pointer(button, 'pointermove', 400, 224)
      expect(button.style.cssText).toBe(position)
      await pointer(button, 'pointerdown', 500, 124)
      await pointer(button, 'pointerup', 500, 124)
      expect(click(button).defaultPrevented).toBe(false)
    }
  })

  it('leaves secondary mouse button gestures alone', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        url: 'https://example.com/docs',
        content_type: 'url',
        page_slug: undefined
      })
    ]
    const { button } = await mountEmbed()
    await pointer(button, 'pointerdown', 700, 24, { button: 2 })
    await pointer(button, 'pointermove', 500, 124)
    expect(button.setPointerCapture).not.toHaveBeenCalled()
    expect(button.style.left).toBe('')
  })

  it('keeps Markdown pages separate from the embedded-page controls', async () => {
    appStoreState.cachedPublicSettings.custom_menu_items = [
      menuItem({
        id: 'docs',
        url: 'md:guide',
        content_type: undefined,
        page_slug: undefined
      })
    ]
    fetchPage.mockResolvedValue({
      ok: true,
      text: async () => '# Guide'
    } as Response)
    const { wrapper } = await mountView('docs')
    expect(wrapper.find('.custom-open-fab').exists()).toBe(false)
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('.markdown-page-content h1').text()).toBe('Guide')
  })
})
