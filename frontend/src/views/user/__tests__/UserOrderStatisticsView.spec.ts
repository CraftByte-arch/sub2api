import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import UserOrderStatisticsView from '../UserOrderStatisticsView.vue'
import type { OrderStatisticsResponse } from '@/types/payment'

const { getOrderStatistics } = vi.hoisted(() => ({
  getOrderStatistics: vi.fn(),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: { getOrderStatistics },
}))

const messages: Record<string, string> = {
  'payment.statistics.title': '订单统计',
  'payment.statistics.refresh': '刷新',
  'payment.statistics.range7': '最近 7 天',
  'payment.statistics.range30': '最近 30 天',
  'payment.statistics.range90': '最近 90 天',
  'payment.statistics.startDate': '开始日期',
  'payment.statistics.endDate': '结束日期',
  'payment.statistics.query': '查询',
  'payment.statistics.range.required': '请选择完整日期',
  'payment.statistics.range.invalid': '日期无效',
  'payment.statistics.range.reversed': '日期顺序无效',
  'payment.statistics.range.tooLong': '日期范围不能超过 366 天',
  'payment.statistics.summary.totalPaid': '总实付金额',
  'payment.statistics.summary.orderCount': '成功订单数',
  'payment.statistics.summary.averagePaid': '平均实付金额',
  'payment.statistics.byType': '类型聚合',
  'payment.statistics.daily': '每日统计',
  'payment.statistics.loadError': '统计加载失败',
  'payment.statistics.retry': '重试',
  'payment.statistics.empty': '该时间范围内没有订单',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
      locale: { value: 'zh-CN' },
    }),
  }
})

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const AggregateTableStub = defineComponent({
  name: 'OrderStatisticsAggregateTable',
  props: { kind: String, rows: Array, currency: String },
  emits: ['select'],
  template: `
    <section :data-test="'aggregate-' + kind">
      <span :data-test="'aggregate-data-' + kind">{{ JSON.stringify(rows) }}</span>
      <button v-if="kind === 'type'" data-test="select-type" @click="$emit('select', { kind: 'type', orderType: 'balance' })">type</button>
      <button v-else data-test="select-date" @click="$emit('select', { kind: 'date', date: '2026-07-20' })">date</button>
    </section>
  `,
})
const DetailsDialogStub = defineComponent({
  name: 'OrderStatisticsDetailsDialog',
  props: { show: Boolean, selection: Object, startDate: String, endDate: String },
  emits: ['close'],
  template: '<div v-if="show" data-test="details-dialog">{{ JSON.stringify({ selection, startDate, endDate }) }}</div>',
})

function statisticsResponse(
  startDate: string,
  endDate: string,
  total: number,
): OrderStatisticsResponse {
  return {
    start_date: startDate,
    end_date: endDate,
    timezone: 'Asia/Shanghai',
    currency: 'CNY',
    summary: {
      total_paid_amount: total,
      order_count: total === 0 ? 0 : 2,
      average_paid_amount: total === 0 ? 0 : total / 2,
    },
    by_type: [
      { order_type: 'balance', total_paid_amount: total, order_count: total === 0 ? 0 : 2, average_paid_amount: total === 0 ? 0 : total / 2 },
      { order_type: 'usage_card', total_paid_amount: 0, order_count: 0, average_paid_amount: 0 },
      { order_type: 'subscription', total_paid_amount: 0, order_count: 0, average_paid_amount: 0 },
    ],
    daily: total === 0
      ? []
      : [{ date: '2026-07-20', total_paid_amount: total, order_count: 2, average_paid_amount: total / 2 }],
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

function mountView() {
  return mount(UserOrderStatisticsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Icon: true,
        OrderStatisticsAggregateTable: AggregateTableStub,
        OrderStatisticsDetailsDialog: DetailsDialogStub,
      },
    },
  })
}

describe('UserOrderStatisticsView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 6, 20, 12, 0, 0))
    getOrderStatistics.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('contains wide aggregate tables without creating page-level horizontal overflow', async () => {
    getOrderStatistics.mockResolvedValue({ data: statisticsResponse('2026-06-21', '2026-07-20', 100) })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="order-statistics-page"]').classes()).toEqual(
      expect.arrayContaining(['min-w-0', 'max-w-full', 'overflow-x-hidden']),
    )
  })

  it('loads the most recent 30 local calendar days by default', async () => {
    getOrderStatistics.mockResolvedValue({ data: statisticsResponse('2026-06-21', '2026-07-20', 100) })

    const wrapper = mountView()
    await flushPromises()

    expect(getOrderStatistics).toHaveBeenCalledWith({
      start_date: '2026-06-21',
      end_date: '2026-07-20',
    })
    expect(wrapper.get('[data-test="applied-range"]').text()).toContain('2026-06-21')
    expect(wrapper.text()).toContain('¥100.00')
  })

  it('queries 7, 30, and 90 day shortcuts immediately', async () => {
    getOrderStatistics.mockResolvedValue({ data: statisticsResponse('2026-06-21', '2026-07-20', 10) })
    const wrapper = mountView()
    await flushPromises()

    for (const [days, startDate] of [[7, '2026-07-14'], [30, '2026-06-21'], [90, '2026-04-22']] as const) {
      await wrapper.get(`[data-test="range-${days}"]`).trigger('click')
      await flushPromises()
      expect(getOrderStatistics).toHaveBeenLastCalledWith({
        start_date: startDate,
        end_date: '2026-07-20',
      })
    }
  })

  it('keeps custom dates as draft until a successful query', async () => {
    getOrderStatistics.mockResolvedValueOnce({ data: statisticsResponse('2026-06-21', '2026-07-20', 10) })
    const custom = deferred<{ data: OrderStatisticsResponse }>()
    getOrderStatistics.mockReturnValueOnce(custom.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="custom-start"]').setValue('2026-07-01')
    await wrapper.get('[data-test="custom-end"]').setValue('2026-07-10')
    expect(getOrderStatistics).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-test="custom-query"]').trigger('click')
    expect(wrapper.get('[data-test="applied-range"]').text()).toContain('2026-06-21')

    custom.resolve({ data: statisticsResponse('2026-07-01', '2026-07-10', 20) })
    await flushPromises()
    expect(wrapper.get('[data-test="applied-range"]').text()).toContain('2026-07-01')
  })

  it('keeps the previous applied range and data when a custom query fails', async () => {
    getOrderStatistics
      .mockResolvedValueOnce({ data: statisticsResponse('2026-06-21', '2026-07-20', 10) })
      .mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="custom-start"]').setValue('2026-07-01')
    await wrapper.get('[data-test="custom-end"]').setValue('2026-07-10')
    await wrapper.get('[data-test="custom-query"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="applied-range"]').text()).toContain('2026-06-21')
    expect(wrapper.text()).toContain('¥10.00')
    expect(wrapper.get('[data-test="statistics-error"]').text()).toContain('统计加载失败')
  })

  it('shows an initial retry state without fabricating zero statistics', async () => {
    getOrderStatistics
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ data: statisticsResponse('2026-06-21', '2026-07-20', 30) })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-test="summary-total"]').exists()).toBe(false)
    await wrapper.get('[data-test="statistics-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="summary-total"]').text()).toContain('¥30.00')
  })

  it('ignores a late aggregate response after a shortcut change', async () => {
    const initial = deferred<{ data: OrderStatisticsResponse }>()
    const latest = deferred<{ data: OrderStatisticsResponse }>()
    getOrderStatistics.mockReturnValueOnce(initial.promise).mockReturnValueOnce(latest.promise)
    const wrapper = mountView()

    await wrapper.get('[data-test="range-7"]').trigger('click')
    latest.resolve({ data: statisticsResponse('2026-07-14', '2026-07-20', 7) })
    await flushPromises()
    initial.resolve({ data: statisticsResponse('2026-06-21', '2026-07-20', 30) })
    await flushPromises()

    expect(wrapper.get('[data-test="applied-range"]').text()).toContain('2026-07-14')
    expect(wrapper.get('[data-test="summary-total"]').text()).toContain('¥7.00')
    expect(wrapper.get('[data-test="summary-total"]').text()).not.toContain('¥30.00')
  })

  it('opens type and date drilldowns with the applied range', async () => {
    getOrderStatistics.mockResolvedValue({ data: statisticsResponse('2026-06-21', '2026-07-20', 100) })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="select-type"]').trigger('click')
    expect(wrapper.get('[data-test="details-dialog"]').text()).toContain('"kind":"type"')
    expect(wrapper.get('[data-test="details-dialog"]').text()).toContain('"startDate":"2026-06-21"')

    await wrapper.get('[data-test="select-date"]').trigger('click')
    expect(wrapper.get('[data-test="details-dialog"]').text()).toContain('"kind":"date"')
    expect(wrapper.get('[data-test="details-dialog"]').text()).toContain('2026-07-20')
  })

  it('renders an explicit no-data state for a successful empty response', async () => {
    getOrderStatistics.mockResolvedValue({ data: statisticsResponse('2026-06-21', '2026-07-20', 0) })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="statistics-empty"]').text()).toContain('该时间范围内没有订单')
    expect(wrapper.get('[data-test="summary-total"]').text()).toContain('¥0.00')
  })
})
