import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UsageCardMini from '../UsageCardMini.vue'

const { getSummary, listMine } = vi.hoisted(() => ({
  getSummary: vi.fn(),
  listMine: vi.fn(),
}))

vi.mock('@/api/usageCards', () => ({
  usageCardsAPI: {
    getSummary,
    listMine,
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'usageCards.availableSummary') return `$${params?.amount} available`
        if (key === 'usageCards.availableCount') return `${params?.count} available cards`
        return key
      },
    }),
  }
})

vi.mock('@/components/icons/Icon.vue', () => ({
  default: {
    props: ['name'],
    template: '<span data-test="icon">{{ name }}</span>',
  },
}))

describe('UsageCardMini', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getSummary.mockReset()
    listMine.mockReset()
    getSummary.mockResolvedValue({
      data: {
        available_count: 2,
        available_remaining_usd: 7.5,
      },
    })
    listMine.mockResolvedValue({ data: [] })
  })

  it('shows available usage-card count and topbar remaining total', async () => {
    const wrapper = mount(UsageCardMini, {
      global: {
        stubs: {
          RouterLink: true,
          transition: false,
        },
      },
    })
    await flushPromises()

    expect(getSummary).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('2')
    expect(wrapper.text()).toContain('$7.50')
  })

  it('keeps the remaining total out of the mobile topbar while preserving it in the summary', async () => {
    const wrapper = mount(UsageCardMini, {
      global: {
        stubs: {
          RouterLink: true,
          transition: false,
        },
      },
    })
    await flushPromises()

    const amount = wrapper.findAll('span').find((span) => span.text() === '$7.50')

    expect(amount?.classes()).toEqual(expect.arrayContaining(['hidden', 'sm:inline']))
    await wrapper.trigger('mouseenter')
    expect(wrapper.text()).toContain('$7.50 available')
  })
})
