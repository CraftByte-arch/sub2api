<template>
  <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
    <table class="w-full min-w-[680px] border-collapse">
      <thead class="bg-gray-50 dark:bg-dark-800">
        <tr>
          <th class="statistics-heading">{{ primaryColumnLabel }}</th>
          <th class="statistics-heading text-right">{{ t('payment.statistics.columns.totalPaid') }}</th>
          <th class="statistics-heading text-right">{{ t('payment.statistics.columns.orderCount') }}</th>
          <th class="statistics-heading text-right">{{ t('payment.statistics.columns.averagePaid') }}</th>
          <th class="w-12 px-3 py-3"><span class="sr-only">{{ t('payment.statistics.openDetails') }}</span></th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
        <tr
          v-for="row in rows"
          :key="rowKey(row)"
          :data-test="`statistics-row-${rowKey(row)}`"
          tabindex="0"
          role="button"
          :aria-label="t('payment.statistics.openDetails')"
          class="h-12 cursor-pointer transition-colors hover:bg-gray-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 dark:hover:bg-dark-800"
          @click="emitSelection(row)"
          @keydown.enter.prevent="emitSelection(row)"
          @keydown.space.prevent="emitSelection(row)"
        >
          <td class="px-4 py-3 text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ primaryValue(row) }}
          </td>
          <td class="px-4 py-3 text-right text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">
            {{ formatAmount(row.total_paid_amount) }}
          </td>
          <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-300">
            {{ row.order_count.toLocaleString(locale) }}
          </td>
          <td class="px-4 py-3 text-right text-sm font-medium tabular-nums text-gray-900 dark:text-gray-100">
            {{ formatAmount(row.average_paid_amount) }}
          </td>
          <td class="px-3 py-3 text-right text-gray-400">
            <Icon name="chevronRight" size="sm" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  DailyOrderStatistics,
  OrderStatisticsDrilldownSelection,
  OrderTypeStatistics,
} from '@/types/payment'
import Icon from '@/components/icons/Icon.vue'
import { formatPaymentAmount } from '@/components/payment/currency'

type AggregateRow = OrderTypeStatistics | DailyOrderStatistics

const props = defineProps<{
  kind: 'type' | 'daily'
  rows: AggregateRow[]
  currency: 'CNY'
}>()

const emit = defineEmits<{
  select: [selection: OrderStatisticsDrilldownSelection]
}>()

const { t, locale } = useI18n()

const primaryColumnLabel = computed(() =>
  props.kind === 'type'
    ? t('payment.statistics.columns.type')
    : t('payment.statistics.columns.date'),
)

function isTypeRow(row: AggregateRow): row is OrderTypeStatistics {
  return 'order_type' in row
}

function rowKey(row: AggregateRow): string {
  return isTypeRow(row) ? row.order_type : row.date
}

function primaryValue(row: AggregateRow): string {
  return isTypeRow(row) ? t(`payment.statistics.types.${row.order_type}`) : row.date
}

function formatAmount(amount: number): string {
  return formatPaymentAmount(amount, props.currency, locale.value)
}

function emitSelection(row: AggregateRow): void {
  if (isTypeRow(row)) {
    emit('select', { kind: 'type', orderType: row.order_type })
    return
  }
  emit('select', { kind: 'date', date: row.date })
}
</script>

<style scoped>
.statistics-heading {
  @apply px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400;
  letter-spacing: 0;
}
</style>
