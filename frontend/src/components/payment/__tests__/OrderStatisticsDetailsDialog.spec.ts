import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import OrderStatisticsDetailsDialog from '../OrderStatisticsDetailsDialog.vue'
import type { BasePaginationResponse } from '@/types'
import type { OrderStatisticsDetail } from '@/types/payment'

const { getOrderStatisticsDetails } = vi.hoisted(() => ({
  getOrderStatisticsDetails: vi.fn(),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: { getOrderStatisticsDetails },
}))

const messages: Record<string, string> = {
  'payment.statistics.details.typeTitle': '类型明细：{type}',
  'payment.statistics.details.dateTitle': '日期明细：{date}',
  'payment.statistics.details.loadError': '明细加载失败',
  'payment.statistics.details.retry': '重试',
  'payment.statistics.details.empty': '没有订单',
  'payment.statistics.columns.orderNo': '订单号',
  'payment.statistics.columns.type': '类型',
  'payment.statistics.columns.paidAmount': '实付金额',
  'payment.statistics.columns.status': '状态',
  'payment.statistics.columns.paymentMethod': '支付方式',
  'payment.statistics.columns.paidAt': '支付时间',
  'payment.statistics.types.balance': '余额',
  'payment.statistics.types.usage_card': '余额卡',
  'payment.statistics.types.subscription': '订阅',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      let message = messages[key] ?? key
      for (const [name, value] of Object.entries(params ?? {})) {
        message = message.replace(`{${name}}`, value)
      }
      return message
    },
    locale: { value: 'zh-CN' },
  }),
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: Boolean, title: String },
  emits: ['close'],
  template: `
    <section v-if="show" data-test="dialog">
      <h2 data-test="dialog-title">{{ title }}</h2>
      <button data-test="dialog-close" @click="$emit('close')">close</button>
      <slot />
    </section>
  `,
})

const DataTableStub = defineComponent({
  name: 'DataTable',
  props: { columns: Array, data: Array, loading: Boolean },
  template: '<div data-test="details-table">{{ JSON.stringify(data) }}</div>',
})

const PaginationStub = defineComponent({
  name: 'Pagination',
  props: { total: Number, page: Number, pageSize: Number, showPageSizeSelector: Boolean },
  emits: ['update:page'],
  template: '<button data-test="details-page-2" @click="$emit(\'update:page\', 2)">next</button>',
})

const detail = (outTradeNo: string): OrderStatisticsDetail => ({
  out_trade_no: outTradeNo,
  order_type: 'balance',
  pay_amount: 12.34,
  status: 'COMPLETED',
  payment_type: 'alipay',
  paid_at: '2026-07-20T01:00:00Z',
})

const page = (items: OrderStatisticsDetail[], currentPage = 1): BasePaginationResponse<OrderStatisticsDetail> => ({
  items,
  total: items.length,
  page: currentPage,
  page_size: 20,
  pages: 1,
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

function mountDialog(selection: { kind: 'type'; orderType: 'balance' } | { kind: 'date'; date: string }) {
  return mount(OrderStatisticsDetailsDialog, {
    props: {
      show: true,
      selection,
      startDate: '2026-07-01',
      endDate: '2026-07-20',
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        OrderStatusBadge: true,
      },
    },
  })
}

describe('OrderStatisticsDetailsDialog', () => {
  beforeEach(() => {
    getOrderStatisticsDetails.mockReset()
  })

  it('loads a fixed-size type page and exposes exactly six columns', async () => {
    getOrderStatisticsDetails.mockResolvedValue({ data: page([detail('type-order')]) })

    const wrapper = mountDialog({ kind: 'type', orderType: 'balance' })
    await flushPromises()

    expect(getOrderStatisticsDetails).toHaveBeenCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-20',
      order_type: 'balance',
      page: 1,
    })
    expect(wrapper.get('[data-test="dialog-title"]').text()).toBe('类型明细：余额')
    expect(wrapper.get('[data-test="details-table"]').text()).toContain('type-order')
    const table = wrapper.findComponent(DataTableStub)
    expect((table.props('columns') as Array<{ key: string }>).map((column) => column.key)).toEqual([
      'out_trade_no',
      'order_type',
      'pay_amount',
      'status',
      'payment_type',
      'paid_at',
    ])
    const pagination = wrapper.findComponent(PaginationStub)
    expect(pagination.props('pageSize')).toBe(20)
    expect(pagination.props('showPageSizeSelector')).toBe(false)
  })

  it('keeps the modal open and retries an in-place failure', async () => {
    getOrderStatisticsDetails
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ data: page([detail('retried-order')]) })

    const wrapper = mountDialog({ kind: 'date', date: '2026-07-20' })
    await flushPromises()
    expect(wrapper.get('[data-test="details-error"]').text()).toContain('明细加载失败')

    await wrapper.get('[data-test="details-retry"]').trigger('click')
    await flushPromises()

    expect(getOrderStatisticsDetails).toHaveBeenLastCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-20',
      date: '2026-07-20',
      page: 1,
    })
    expect(wrapper.get('[data-test="details-table"]').text()).toContain('retried-order')
  })

  it('ignores a late response after switching drilldown selection', async () => {
    const first = deferred<{ data: BasePaginationResponse<OrderStatisticsDetail> }>()
    const second = deferred<{ data: BasePaginationResponse<OrderStatisticsDetail> }>()
    getOrderStatisticsDetails.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = mountDialog({ kind: 'type', orderType: 'balance' })

    await wrapper.setProps({ selection: { kind: 'date', date: '2026-07-20' } })
    second.resolve({ data: page([detail('new-selection')]) })
    await flushPromises()
    first.resolve({ data: page([detail('stale-selection')]) })
    await flushPromises()

    expect(wrapper.get('[data-test="details-table"]').text()).toContain('new-selection')
    expect(wrapper.get('[data-test="details-table"]').text()).not.toContain('stale-selection')
  })

  it('resets pagination after close and reopen', async () => {
    getOrderStatisticsDetails.mockResolvedValue({ data: page([detail('order')]) })
    const wrapper = mountDialog({ kind: 'type', orderType: 'balance' })
    await flushPromises()

    await wrapper.get('[data-test="details-page-2"]').trigger('click')
    await flushPromises()
    expect(getOrderStatisticsDetails).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2 }))

    await wrapper.get('[data-test="dialog-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getOrderStatisticsDetails).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1 }))
  })
})
