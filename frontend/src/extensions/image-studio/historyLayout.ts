const DEFAULT_HISTORY_COLUMN_WIDTH = 360
const DEFAULT_HISTORY_COLUMN_GAP = 14

export function groupHistoryRecordsByVisualColumn<T>(records: readonly T[], columnCount: number): T[][] {
  const columns = Math.max(1, Math.floor(columnCount))
  if (columns <= 1) return [[...records]]

  const grouped = Array.from({ length: columns }, () => [] as T[])

  records.forEach((record, index) => {
    grouped[index % columns].push(record)
  })

  return grouped.filter((column) => column.length > 0)
}

export function estimateHistoryColumnCount(
  containerWidth: number,
  columnWidth = DEFAULT_HISTORY_COLUMN_WIDTH,
  columnGap = DEFAULT_HISTORY_COLUMN_GAP
): number {
  if (!Number.isFinite(containerWidth) || containerWidth <= 0) return 1
  const effectiveColumnWidth = Number.isFinite(columnWidth) && columnWidth > 0
    ? columnWidth
    : DEFAULT_HISTORY_COLUMN_WIDTH
  const effectiveColumnGap = Number.isFinite(columnGap) && columnGap >= 0
    ? columnGap
    : DEFAULT_HISTORY_COLUMN_GAP

  return Math.max(1, Math.floor((containerWidth + effectiveColumnGap) / (effectiveColumnWidth + effectiveColumnGap)))
}
