import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MyInvitesView from '../MyInvitesView.vue'

const { getMyInvitees, showError } = vi.hoisted(() => ({
  getMyInvitees: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  default: {
    getMyInvitees,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('@/utils/format', () => ({
  formatCurrency: (value: number) => `$${value.toFixed(2)}`,
  formatDateTime: (value: string) => value,
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

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'data', 'loading'],
  template: `
    <section data-test="invitees-table">
      <div v-if="loading" data-test="loading" />
      <template v-else-if="data.length">
        <article v-for="row in data" :key="row.user_id">
          <slot name="cell-email" :value="row.email" :row="row" />
          <slot name="cell-username" :value="row.username" :row="row" />
          <slot name="cell-total_rebate" :value="row.total_rebate" :row="row" />
          <slot name="cell-created_at" :value="row.created_at" :row="row" />
        </article>
      </template>
      <slot v-else name="empty" />
    </section>
  `,
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div data-test="pagination">
      <button data-test="next-page" @click="$emit('update:page', page + 1)">next</button>
      <button data-test="change-page-size" @click="$emit('update:pageSize', 50)">50</button>
    </div>
  `,
}

function mountView() {
  return mount(MyInvitesView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        Icon: true,
      },
    },
  })
}

describe('MyInvitesView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the unmasked invitee email returned for the current expert and paginates', async () => {
    getMyInvitees
      .mockResolvedValueOnce({
        items: [{
          user_id: 7,
          email: 'invitee.original@example.com',
          username: 'invitee',
          total_rebate: 12.5,
          created_at: '2026-07-31T12:00:00Z',
        }],
        total: 42,
        page: 1,
        page_size: 20,
        pages: 3,
      })
      .mockResolvedValueOnce({
        items: [{
          user_id: 8,
          email: 'second.invitee@example.com',
          username: 'second-invitee',
          total_rebate: 8,
          created_at: '2026-07-30T12:00:00Z',
        }],
        total: 42,
        page: 2,
        page_size: 20,
        pages: 3,
      })
      .mockResolvedValueOnce({
        items: [],
        total: 42,
        page: 1,
        page_size: 50,
        pages: 1,
      })

    const wrapper = mountView()
    await flushPromises()

    expect(getMyInvitees).toHaveBeenCalledWith({ page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('invitee.original@example.com')
    expect(wrapper.text()).toContain('$12.50')

    await wrapper.get('[data-test="next-page"]').trigger('click')
    await flushPromises()

    expect(getMyInvitees).toHaveBeenLastCalledWith({ page: 2, page_size: 20 })
    expect(wrapper.text()).toContain('second.invitee@example.com')

    await wrapper.get('[data-test="change-page-size"]').trigger('click')
    await flushPromises()

    expect(getMyInvitees).toHaveBeenLastCalledWith({ page: 1, page_size: 50 })
  })
})
