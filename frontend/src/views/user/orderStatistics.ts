export interface LocalDateRange {
  startDate: string
  endDate: string
}

export type DateRangeValidationError = 'required' | 'invalid' | 'reversed' | 'tooLong'

const DATE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/
const DAY_MS = 24 * 60 * 60 * 1000
const MAX_RANGE_DAYS = 366

export function formatLocalDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function rangeForLastDays(days: number, now = new Date()): LocalDateRange {
  if (!Number.isInteger(days) || days < 1) {
    throw new RangeError('days must be a positive integer')
  }
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const start = new Date(end)
  start.setDate(start.getDate() - (days - 1))
  return {
    startDate: formatLocalDate(start),
    endDate: formatLocalDate(end),
  }
}

export function validateInclusiveRange(
  startDate: string,
  endDate: string,
): DateRangeValidationError | null {
  if (!startDate || !endDate) return 'required'
  const startDay = calendarDayNumber(startDate)
  const endDay = calendarDayNumber(endDate)
  if (startDay === null || endDay === null) return 'invalid'
  if (endDay < startDay) return 'reversed'
  if (endDay - startDay + 1 > MAX_RANGE_DAYS) return 'tooLong'
  return null
}

function calendarDayNumber(value: string): number | null {
  const match = DATE_PATTERN.exec(value)
  if (!match) return null
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  const candidate = new Date(year, month - 1, day)
  if (
    candidate.getFullYear() !== year ||
    candidate.getMonth() !== month - 1 ||
    candidate.getDate() !== day
  ) {
    return null
  }
  return Date.UTC(year, month - 1, day) / DAY_MS
}
