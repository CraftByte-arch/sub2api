<template>
  <div class="relative" ref="containerRef">
    <button
      type="button"
      :aria-expanded="isOpen"
      :aria-haspopup="'dialog'"
      @click="toggle"
      :class="['date-picker-trigger', props.block && 'date-picker-trigger-block', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Transition name="date-picker-dropdown">
      <div v-if="isOpen" class="date-picker-dropdown">
        <div v-if="props.showPresets" class="date-picker-presets">
          <button
            type="button"
            v-for="preset in presets"
            :key="preset.value"
            @click="selectPreset(preset)"
            :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
          >
            {{ t(preset.labelKey) }}
          </button>
        </div>

        <div v-if="props.showPresets" class="date-picker-divider"></div>

        <div class="date-picker-calendar" role="dialog" :aria-label="t('dates.calendar')">
          <div class="date-picker-calendar-header">
            <button
              type="button"
              class="date-picker-nav"
              :disabled="!canGoPrevious"
              :aria-label="t('dates.previousMonth')"
              @click="moveMonth(-1)"
            >
              <Icon name="chevronLeft" size="sm" />
            </button>
            <span class="date-picker-month-label">{{ calendarMonthLabel }}</span>
            <button
              type="button"
              class="date-picker-nav"
              :disabled="!canGoNext"
              :aria-label="t('dates.nextMonth')"
              @click="moveMonth(1)"
            >
              <Icon name="chevronRight" size="sm" />
            </button>
          </div>

          <div class="date-picker-weekdays" aria-hidden="true">
            <span v-for="weekday in weekdayLabels" :key="weekday" class="date-picker-weekday">
              {{ weekday }}
            </span>
          </div>

          <div class="date-picker-grid">
            <button
              v-for="day in calendarDays"
              :key="day.value"
              type="button"
              class="date-picker-day"
              :data-date="day.value"
              :class="{
                'date-picker-day-muted': !day.inCurrentMonth,
                'date-picker-day-range': day.isInRange,
                'date-picker-day-selected': day.isSelectedStart || day.isSelectedEnd,
                'date-picker-day-today': day.isToday,
              }"
              :disabled="day.disabled"
              :aria-current="day.isToday ? 'date' : undefined"
              :aria-pressed="day.isSelectedStart || day.isSelectedEnd"
              :aria-label="accessibleDate(day.date)"
              @click="selectDate(day)"
            >
              {{ day.day }}
            </button>
          </div>

          <div class="date-picker-actions">
            <span class="date-picker-selection-hint">{{ selectionHint }}</span>
            <button type="button" @click="apply" class="date-picker-apply" :disabled="!hasCompleteRange">
              {{ t('dates.apply') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => { start: string; end: string }
}

interface Props {
  startDate: string
  endDate: string
  showPresets?: boolean
  block?: boolean
  minDate?: string
  maxDate?: string
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (e: 'change', range: { startDate: string; endDate: string; preset: string | null }): void
}

const props = withDefaults(defineProps<Props>(), {
  showPresets: true,
  block: false,
  minDate: '',
  maxDate: '',
})
const emit = defineEmits<Emits>()

const { t, locale } = useI18n()

const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
const activePreset = ref<string | null>('last24Hours')
const selectionStep = ref<'start' | 'end'>('start')

const formatDateToString = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const dateFromString = (value: string): Date | null => {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null
  const [year, month, day] = value.split('-').map(Number)
  const date = new Date(year, month - 1, day)
  return date.getFullYear() === year && date.getMonth() === month - 1 && date.getDate() === day ? date : null
}

const startOfMonth = (date: Date): Date => new Date(date.getFullYear(), date.getMonth(), 1)

const today = computed(() => formatDateToString(new Date()))

// Keep one extra local day available for the existing usage dashboards.
const tomorrow = computed(() => {
  const date = new Date()
  date.setDate(date.getDate() + 1)
  return formatDateToString(date)
})

const maxSelectableDate = computed(() => props.maxDate || tomorrow.value)
const minSelectableDate = computed(() => props.minDate || '')
const dateLocale = computed(() => locale.value.startsWith('zh') ? 'zh-CN' : 'en-US')
const calendarMonth = ref(startOfMonth(dateFromString(props.startDate) || dateFromString(props.endDate) || new Date()))

interface CalendarDay {
  date: Date
  value: string
  day: number
  inCurrentMonth: boolean
  disabled: boolean
  isToday: boolean
  isInRange: boolean
  isSelectedStart: boolean
  isSelectedEnd: boolean
}

const weekdayLabels = computed(() => locale.value.startsWith('zh')
  ? ['日', '一', '二', '三', '四', '五', '六']
  : ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'])

const presets: DatePreset[] = [
  {
    labelKey: 'dates.today',
    value: 'today',
    getRange: () => {
      const t = today.value
      return { start: t, end: t }
    }
  },
  {
    labelKey: 'dates.yesterday',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const yesterday = formatDateToString(d)
      return { start: yesterday, end: yesterday }
    }
  },
  {
    labelKey: 'dates.last24Hours',
    value: 'last24Hours',
    getRange: () => {
      const end = new Date()
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      return {
        start: formatDateToString(start),
        end: formatDateToString(end)
      }
    }
  },
  {
    labelKey: 'dates.last7Days',
    value: '7days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 6)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last14Days',
    value: '14days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 13)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last30Days',
    value: '30days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 29)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.thisMonth',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1))
      return { start, end: today.value }
    }
  },
  {
    labelKey: 'dates.lastMonth',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1))
      const end = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0))
      return { start, end }
    }
  }
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const preset = presets.find((p) => p.value === activePreset.value)
    if (preset) return t(preset.labelKey)
  }

  if (localStartDate.value && localEndDate.value) {
    if (localStartDate.value === localEndDate.value) {
      return formatDate(localStartDate.value)
    }
    return `${formatDate(localStartDate.value)} - ${formatDate(localEndDate.value)}`
  }

  return t('dates.selectDateRange')
})

const calendarMonthLabel = computed(() => calendarMonth.value.toLocaleDateString(dateLocale.value, {
  year: 'numeric',
  month: 'long',
}))

const calendarDays = computed<CalendarDay[]>(() => {
  const month = calendarMonth.value
  const firstDay = startOfMonth(month)
  const gridStart = new Date(firstDay)
  gridStart.setDate(firstDay.getDate() - firstDay.getDay())
  const minimum = minSelectableDate.value
  const maximum = maxSelectableDate.value

  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(gridStart)
    date.setDate(gridStart.getDate() + index)
    const value = formatDateToString(date)
    const inCurrentMonth = date.getMonth() === month.getMonth() && date.getFullYear() === month.getFullYear()
    return {
      date,
      value,
      day: date.getDate(),
      inCurrentMonth,
      disabled: (minimum !== '' && value < minimum) || (maximum !== '' && value > maximum),
      isToday: value === today.value,
      isInRange: Boolean(localStartDate.value && localEndDate.value && value > localStartDate.value && value < localEndDate.value),
      isSelectedStart: value === localStartDate.value,
      isSelectedEnd: Boolean(localEndDate.value && value === localEndDate.value),
    }
  })
})

const hasCompleteRange = computed(() => Boolean(localStartDate.value && localEndDate.value))
const selectionHint = computed(() => {
  if (!localStartDate.value) return t('dates.selectStartDate')
  if (!localEndDate.value) return t('dates.selectEndDate')
  return displayValue.value
})

const canGoPrevious = computed(() => {
  const minimum = dateFromString(minSelectableDate.value)
  return !minimum || calendarMonth.value > startOfMonth(minimum)
})

const canGoNext = computed(() => {
  const maximum = dateFromString(maxSelectableDate.value)
  return !maximum || calendarMonth.value < startOfMonth(maximum)
})

const formatDate = (dateStr: string): string => {
  const date = dateFromString(dateStr)
  if (!date) return dateStr
  return date.toLocaleDateString(dateLocale.value, { month: 'short', day: 'numeric' })
}

const accessibleDate = (date: Date): string => date.toLocaleDateString(dateLocale.value, {
  year: 'numeric',
  month: 'long',
  day: 'numeric',
})

const isPresetActive = (preset: DatePreset): boolean => {
  return activePreset.value === preset.value
}

const selectPreset = (preset: DatePreset) => {
  const range = preset.getRange()
  localStartDate.value = range.start
  localEndDate.value = range.end
  activePreset.value = preset.value
  selectionStep.value = 'start'
  const anchor = dateFromString(range.start)
  if (anchor) calendarMonth.value = startOfMonth(anchor)
}

const onDateChange = () => {
  // Check if current dates match any preset
  activePreset.value = null
  for (const preset of presets) {
    const range = preset.getRange()
    if (range.start === localStartDate.value && range.end === localEndDate.value) {
      activePreset.value = preset.value
      break
    }
  }
  selectionStep.value = localStartDate.value && !localEndDate.value ? 'end' : 'start'
}

const selectDate = (day: CalendarDay) => {
  if (day.disabled) return

  if (selectionStep.value === 'start' || !localStartDate.value || localEndDate.value) {
    localStartDate.value = day.value
    localEndDate.value = ''
    selectionStep.value = 'end'
  } else if (day.value < localStartDate.value) {
    localStartDate.value = day.value
    localEndDate.value = ''
    selectionStep.value = 'end'
  } else {
    localEndDate.value = day.value
    selectionStep.value = 'start'
  }

  activePreset.value = null
  if (!day.inCurrentMonth) calendarMonth.value = startOfMonth(day.date)
}

const moveMonth = (offset: number) => {
  const next = new Date(calendarMonth.value)
  next.setMonth(next.getMonth() + offset)
  if (offset < 0 && !canGoPrevious.value) return
  if (offset > 0 && !canGoNext.value) return
  calendarMonth.value = startOfMonth(next)
}

const toggle = () => {
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    const anchor = dateFromString(localStartDate.value) || dateFromString(localEndDate.value) || new Date()
    calendarMonth.value = startOfMonth(anchor)
    selectionStep.value = localStartDate.value && localEndDate.value ? 'start' : localStartDate.value ? 'end' : 'start'
  }
}

const apply = () => {
  if (!hasCompleteRange.value) return
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    preset: activePreset.value
  })
  isOpen.value = false
}

const handleClickOutside = (event: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    isOpen.value = false
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && isOpen.value) {
    isOpen.value = false
  }
}

watch(
  () => [props.startDate, props.endDate],
  ([startDate, endDate]) => {
    localStartDate.value = startDate
    localEndDate.value = endDate
    onDateChange()
    if (!isOpen.value) {
      const anchor = dateFromString(startDate) || dateFromString(endDate)
      if (anchor) calendarMonth.value = startOfMonth(anchor)
    }
  },
  { immediate: true },
)

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
.date-picker-trigger {
  @apply flex items-center gap-2;
  @apply min-h-11 rounded-lg px-3 py-2 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-700 dark:text-gray-300;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.date-picker-trigger-block {
  @apply w-full justify-between;
}

.date-picker-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.date-picker-icon {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-value {
  @apply font-medium;
}

.date-picker-chevron {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-dropdown {
  @apply absolute left-0 z-[100] mt-2;
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
  width: min(23rem, calc(100vw - 1rem));
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply rounded-md px-3 py-1.5 text-xs font-medium;
  @apply text-gray-600 dark:text-gray-400;
  @apply hover:bg-gray-100 dark:hover:bg-dark-700;
  @apply transition-colors duration-150;
}

.date-picker-preset-active {
  @apply bg-primary-100 dark:bg-primary-900/30;
  @apply text-primary-700 dark:text-primary-300;
}

.date-picker-divider {
  @apply border-t border-gray-100 dark:border-dark-700;
}

.date-picker-calendar {
  @apply px-3 pb-3 pt-2;
}

.date-picker-calendar-header {
  @apply flex items-center justify-between gap-2;
}

.date-picker-month-label {
  @apply min-w-0 flex-1 text-center text-sm font-semibold text-gray-900 dark:text-gray-100;
}

.date-picker-nav {
  @apply flex h-10 w-10 items-center justify-center rounded-md text-gray-500 transition-colors duration-150;
  @apply hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-100;
  @apply focus:outline-none focus:ring-2 focus:ring-primary-500/40;
}

.date-picker-nav:disabled {
  @apply cursor-not-allowed opacity-30;
}

.date-picker-weekdays,
.date-picker-grid {
  @apply grid grid-cols-7 gap-1;
}

.date-picker-weekdays {
  @apply mb-1 mt-2;
}

.date-picker-weekday {
  @apply flex h-8 items-center justify-center text-xs font-medium text-gray-400 dark:text-gray-500;
}

.date-picker-day {
  @apply flex min-h-11 items-center justify-center rounded-md text-sm font-medium tabular-nums text-gray-700 transition-colors duration-150;
  @apply hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700;
  @apply focus:outline-none focus:ring-2 focus:ring-primary-500/40;
}

.date-picker-day-muted {
  @apply text-gray-400 dark:text-gray-600;
}

.date-picker-day-range {
  @apply bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-200;
}

.date-picker-day-selected,
.date-picker-day-selected:hover {
  @apply bg-primary-600 text-white dark:bg-primary-500 dark:text-white;
}

.date-picker-day-today:not(.date-picker-day-selected) {
  @apply ring-1 ring-inset ring-primary-500;
}

.date-picker-day:disabled {
  @apply cursor-not-allowed opacity-30;
}

.date-picker-actions {
  @apply mt-2 flex min-h-11 items-center justify-between gap-3 border-t border-gray-100 pt-3 dark:border-dark-700;
}

.date-picker-selection-hint {
  @apply min-w-0 truncate text-xs text-gray-500 dark:text-gray-400;
}

.date-picker-apply {
  @apply min-h-10 rounded-lg px-4 py-1.5 text-sm font-medium;
  @apply bg-primary-600 text-white;
  @apply hover:bg-primary-700;
  @apply focus:outline-none focus:ring-2 focus:ring-primary-500/40;
  @apply transition-colors duration-150;
}

.date-picker-apply:disabled {
  @apply cursor-not-allowed opacity-50;
}

/* Dropdown animation */
.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition: all 0.2s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

@media (max-width: 480px) {
  .date-picker-dropdown {
    left: 50%;
    transform: translateX(-50%);
  }

  .date-picker-dropdown-enter-from,
  .date-picker-dropdown-leave-to {
    transform: translate(-50%, -8px);
  }
}
</style>
