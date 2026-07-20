import { describe, expect, it } from 'vitest'

import {
  formatLocalDate,
  rangeForLastDays,
  validateInclusiveRange,
} from '@/views/user/orderStatistics'

describe('order statistics local date helpers', () => {
  it('builds an inclusive last-30-days range from local calendar dates', () => {
    expect(rangeForLastDays(30, new Date(2026, 6, 20, 12, 0, 0))).toEqual({
      startDate: '2026-06-21',
      endDate: '2026-07-20',
    })
  })

  it('formats dates without converting them through UTC', () => {
    expect(formatLocalDate(new Date(2026, 0, 2, 0, 30, 0))).toBe('2026-01-02')
  })

  it('accepts at most 366 inclusive calendar days', () => {
    expect(validateInclusiveRange('2025-07-20', '2026-07-20')).toBeNull()
    expect(validateInclusiveRange('2025-07-19', '2026-07-20')).toBe('tooLong')
  })

  it.each([
    ['', '2026-07-20', 'required'],
    ['2026-07-01', '', 'required'],
    ['2026-02-30', '2026-07-20', 'invalid'],
    ['2026-07-21', '2026-07-20', 'reversed'],
  ])('rejects invalid range %s to %s', (startDate, endDate, expected) => {
    expect(validateInclusiveRange(startDate, endDate)).toBe(expected)
  })
})
