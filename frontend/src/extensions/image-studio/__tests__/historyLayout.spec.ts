import { describe, expect, it } from 'vitest'

import { estimateHistoryColumnCount, groupHistoryRecordsByVisualColumn } from '../historyLayout'

describe('historyLayout', () => {
  it('groups records into explicit columns so each visual row stays newest to oldest', () => {
    const records = Array.from({ length: 8 }, (_, index) => ({ id: String(index + 1) }))

    expect(groupHistoryRecordsByVisualColumn(records, 4).map((column) => column.map((record) => record.id))).toEqual([
      ['1', '5'],
      ['2', '6'],
      ['3', '7'],
      ['4', '8']
    ])
  })

  it('keeps the original order when there is only one column', () => {
    const records = Array.from({ length: 5 }, (_, index) => ({ id: String(index + 1) }))

    expect(groupHistoryRecordsByVisualColumn(records, 1).map((column) => column.map((record) => record.id))).toEqual([
      ['1', '2', '3', '4', '5']
    ])
  })

  it('keeps rows visually ordered when the final row is not full', () => {
    const records = Array.from({ length: 7 }, (_, index) => ({ id: String(index + 1) }))

    expect(groupHistoryRecordsByVisualColumn(records, 3).map((column) => column.map((record) => record.id))).toEqual([
      ['1', '4', '7'],
      ['2', '5'],
      ['3', '6']
    ])
  })

  it('estimates the browser column count from measured column width and gap', () => {
    expect(estimateHistoryColumnCount(1510, 360, 14)).toBe(4)
    expect(estimateHistoryColumnCount(1080, 360, 14)).toBe(2)
    expect(estimateHistoryColumnCount(640, 280, 14)).toBe(2)
  })
})
