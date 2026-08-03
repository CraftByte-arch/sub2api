import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { UserUsageCard } from '@/api/usageCards'
import UsageCardsView from '../UsageCardsView.vue'

const { listPlans, listCards, showError } = vi.hoisted(() => ({
  listPlans: vi.fn(),
  listCards: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/usageCards', () => ({
  adminUsageCardsAPI: {
    listPlans,
    listCards,
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
    deletePlan: vi.fn(),
    cancelCard: vi.fn(),
    convertCardToBalance: vi.fn(),
    suspendCard: vi.fn(),
    resumeCard: vi.fn(),
  },
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: {
      searchUsers: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const DataTableStub = defineComponent({
  name: 'DataTable',
  props: {
    data: { type: Array, default: () => [] },
    loading: Boolean,
    expandableActions: Boolean,
    serverSideSort: Boolean,
  },
  emits: ['sort'],
  template: `
    <section data-test="cards-table">
      <span data-test="card-ids">{{ data.map((row) => row.id).join(',') }}</span>
      <span data-test="cards-loading">{{ loading }}</span>
    </section>
  `,
})

const SelectStub = defineComponent({
  name: 'StatusSelectStub',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: '<button type="button" data-test="status-select">select</button>',
})

const PaginationStub = defineComponent({
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: '<div data-test="pagination" />',
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function card(id: number, status = 'active'): UserUsageCard {
  return {
    id,
    user_id: id,
    name: `Card ${id}`,
    starts_at: '2026-08-01T00:00:00Z',
    expires_at: '2026-09-01T00:00:00Z',
    total_limit_usd: 100,
    used_usd: 10,
    remaining_usd: 90,
    status,
    source: 'payment',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  }
}

function page(items: UserUsageCard[], total = items.length, pageNumber = 1, pageSize = 20) {
  return {
    data: {
      items,
      total,
      page: pageNumber,
      page_size: pageSize,
      pages: Math.max(1, Math.ceil(total / pageSize)),
    },
  }
}

function mountView() {
  return mount(UsageCardsView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        BaseDialog: true,
        ConfirmDialog: true,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        Select: SelectStub,
        Icon: true,
      },
    },
  })
}

describe('admin UsageCardsView filtering', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listPlans.mockResolvedValue({ data: [] })
  })

  it('keeps only the latest filter response and does not clear its loading state early', async () => {
    listCards.mockResolvedValueOnce(page([card(1)]))
    const wrapper = mountView()
    await flushPromises()

    const staleRequest = deferred<ReturnType<typeof page>>()
    const latestRequest = deferred<ReturnType<typeof page>>()
    listCards
      .mockReturnValueOnce(staleRequest.promise)
      .mockReturnValueOnce(latestRequest.promise)

    const statusSelect = wrapper.getComponent(SelectStub)
    statusSelect.vm.$emit('update:modelValue', 'expired')
    statusSelect.vm.$emit('change', 'expired', null)
    await nextTick()

    statusSelect.vm.$emit('update:modelValue', 'cancelled')
    statusSelect.vm.$emit('change', 'cancelled', null)
    await nextTick()

    const staleSignal = listCards.mock.calls[1][1].signal as AbortSignal
    expect(staleSignal.aborted).toBe(true)

    staleRequest.resolve(page([card(2, 'expired')]))
    await flushPromises()

    expect(wrapper.get('[data-test="card-ids"]').text()).toBe('1')
    expect(wrapper.get('[data-test="cards-loading"]').text()).toBe('true')

    latestRequest.resolve(page([card(3, 'cancelled')]))
    await flushPromises()

    expect(wrapper.get('[data-test="card-ids"]').text()).toBe('3')
    expect(wrapper.get('[data-test="cards-loading"]').text()).toBe('false')
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.getComponent(DataTableStub).props('expandableActions')).toBe(false)
  })

  it('loads one server page at a time and resets to page one for filters and sorting', async () => {
    listCards.mockResolvedValueOnce(page([card(1)], 45))
    const wrapper = mountView()
    await flushPromises()

    listCards.mockResolvedValueOnce(page([card(21)], 45, 2))
    wrapper.getComponent(PaginationStub).vm.$emit('update:page', 2)
    await flushPromises()

    expect(listCards.mock.calls[1][0]).toEqual(expect.objectContaining({ page: 2 }))

    listCards.mockResolvedValueOnce(page([card(2, 'expired')], 1))
    const statusSelect = wrapper.getComponent(SelectStub)
    statusSelect.vm.$emit('update:modelValue', 'expired')
    statusSelect.vm.$emit('change', 'expired', null)
    await flushPromises()

    expect(listCards.mock.calls[2][0]).toEqual(expect.objectContaining({
      page: 1,
      status: 'expired',
    }))

    listCards.mockResolvedValueOnce(page([card(4, 'expired')], 1))
    wrapper.getComponent(DataTableStub).vm.$emit('sort', 'name', 'desc')
    await flushPromises()

    expect(listCards.mock.calls[3][0]).toEqual(expect.objectContaining({
      page: 1,
      sort_by: 'name',
      sort_order: 'desc',
    }))
    expect(wrapper.getComponent(DataTableStub).props('serverSideSort')).toBe(true)
  })
})
