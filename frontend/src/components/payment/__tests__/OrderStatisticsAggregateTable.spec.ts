import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import OrderStatisticsAggregateTable from '../OrderStatisticsAggregateTable.vue'

const messages: Record<string, string> = {
  'payment.statistics.columns.type': '类型',
  'payment.statistics.columns.date': '日期',
  'payment.statistics.columns.totalPaid': '总实付',
  'payment.statistics.columns.orderCount': '订单数',
  'payment.statistics.columns.averagePaid': '平均实付',
  'payment.statistics.types.balance': '余额',
  'payment.statistics.types.usage_card': '余额卡',
  'payment.statistics.types.subscription': '订阅',
  'payment.statistics.openDetails': '查看明细',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => messages[key] ?? key,
    locale: { value: 'zh-CN' },
  }),
}))

describe('OrderStatisticsAggregateTable', () => {
  it('emits a type selection from click, Enter, and Space', async () => {
    const wrapper = mount(OrderStatisticsAggregateTable, {
      props: {
        kind: 'type',
        currency: 'CNY',
        rows: [
          { order_type: 'balance', total_paid_amount: 100, order_count: 2, average_paid_amount: 50 },
          { order_type: 'usage_card', total_paid_amount: 20, order_count: 1, average_paid_amount: 20 },
          { order_type: 'subscription', total_paid_amount: 0, order_count: 0, average_paid_amount: 0 },
        ],
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.text()).toContain('余额')
    expect(wrapper.text()).toContain('余额卡')
    expect(wrapper.text()).toContain('订阅')
    const row = wrapper.get('[data-test="statistics-row-balance"]')
    expect(row.attributes('tabindex')).toBe('0')

    await row.trigger('click')
    await row.trigger('keydown', { key: 'Enter' })
    await row.trigger('keydown', { key: ' ' })

    expect(wrapper.emitted('select')).toEqual([
      [{ kind: 'type', orderType: 'balance' }],
      [{ kind: 'type', orderType: 'balance' }],
      [{ kind: 'type', orderType: 'balance' }],
    ])
  })

  it('emits a date selection for a daily row', async () => {
    const wrapper = mount(OrderStatisticsAggregateTable, {
      props: {
        kind: 'daily',
        currency: 'CNY',
        rows: [
          { date: '2026-07-20', total_paid_amount: 88.5, order_count: 3, average_paid_amount: 29.5 },
        ],
      },
      global: { stubs: { Icon: true } },
    })

    await wrapper.get('[data-test="statistics-row-2026-07-20"]').trigger('click')

    expect(wrapper.emitted('select')).toEqual([[{ kind: 'date', date: '2026-07-20' }]])
  })
})
