<template>
  <BaseDialog :show="show" :title="dialogTitle" width="extra-wide" @close="handleClose">
    <div class="min-h-64">
      <div
        v-if="error"
        data-test="details-error"
        class="flex min-h-64 flex-col items-center justify-center gap-3 text-center"
      >
        <p class="text-sm text-red-600 dark:text-red-400">{{ t('payment.statistics.details.loadError') }}</p>
        <button data-test="details-retry" type="button" class="btn btn-secondary" @click="loadPage(page)">
          {{ t('payment.statistics.details.retry') }}
        </button>
      </div>

      <template v-else>
        <div class="overflow-x-auto">
          <DataTable :columns="columns" :data="items" :loading="loading">
            <template #cell-out_trade_no="{ value }">
              <span class="font-mono text-xs text-gray-900 dark:text-gray-100">{{ value }}</span>
            </template>
            <template #cell-order_type="{ value }">
              <span class="badge badge-gray">{{ typeLabel(value) }}</span>
            </template>
            <template #cell-pay_amount="{ value }">
              <span class="font-medium tabular-nums">{{ formatAmount(value) }}</span>
            </template>
            <template #cell-status="{ value }">
              <OrderStatusBadge :status="value" />
            </template>
            <template #cell-payment_type="{ value }">
              {{ t(`payment.methods.${value}`, value) }}
            </template>
            <template #cell-paid_at="{ value }">
              <span class="whitespace-nowrap text-xs text-gray-600 dark:text-gray-300">{{ formatPaidAt(value) }}</span>
            </template>
            <template #empty>
              <p class="py-6 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.statistics.details.empty') }}</p>
            </template>
          </DataTable>
        </div>
        <Pagination
          v-if="total > 0"
          :total="total"
          :page="page"
          :page-size="DETAIL_PAGE_SIZE"
          :show-page-size-selector="false"
          @update:page="loadPage"
        />
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import type {
  OrderStatisticsDetail,
  OrderStatisticsDetailsParams,
  OrderStatisticsDrilldownSelection,
  OrderType,
} from '@/types/payment'
import { paymentAPI } from '@/api/payment'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import { formatPaymentAmount } from '@/components/payment/currency'

const DETAIL_PAGE_SIZE = 20

const props = defineProps<{
  show: boolean
  selection: OrderStatisticsDrilldownSelection
  startDate: string
  endDate: string
}>()

const emit = defineEmits<{
  close: []
}>()

const { t, locale } = useI18n()
const items = ref<OrderStatisticsDetail[]>([])
const loading = ref(false)
const error = ref(false)
const page = ref(1)
const total = ref(0)
let requestGeneration = 0

const columns = computed((): Column[] => [
  { key: 'out_trade_no', label: t('payment.statistics.columns.orderNo') },
  { key: 'order_type', label: t('payment.statistics.columns.type') },
  { key: 'pay_amount', label: t('payment.statistics.columns.paidAmount') },
  { key: 'status', label: t('payment.statistics.columns.status') },
  { key: 'payment_type', label: t('payment.statistics.columns.paymentMethod') },
  { key: 'paid_at', label: t('payment.statistics.columns.paidAt') },
])

const selectionKey = computed(() =>
  props.selection.kind === 'type'
    ? `type:${props.selection.orderType}`
    : `date:${props.selection.date}`,
)

const dialogTitle = computed(() => {
  if (props.selection.kind === 'type') {
    return t('payment.statistics.details.typeTitle', {
      type: typeLabel(props.selection.orderType),
    })
  }
  return t('payment.statistics.details.dateTitle', { date: props.selection.date })
})

watch(
  [() => props.show, selectionKey, () => props.startDate, () => props.endDate],
  ([show]) => {
    requestGeneration++
    page.value = 1
    items.value = []
    total.value = 0
    error.value = false
    loading.value = false
    if (show) void loadPage(1)
  },
  { immediate: true },
)

async function loadPage(nextPage: number): Promise<void> {
  const generation = ++requestGeneration
  page.value = nextPage
  loading.value = true
  error.value = false
  try {
    const params = detailParams(nextPage)
    const response = await paymentAPI.getOrderStatisticsDetails(params)
    if (generation !== requestGeneration || !props.show) return
    items.value = response.data.items ?? []
    total.value = response.data.total ?? 0
    page.value = response.data.page || nextPage
  } catch {
    if (generation !== requestGeneration || !props.show) return
    error.value = true
  } finally {
    if (generation === requestGeneration) loading.value = false
  }
}

function detailParams(nextPage: number): OrderStatisticsDetailsParams {
  const range = {
    start_date: props.startDate,
    end_date: props.endDate,
    page: nextPage,
  }
  if (props.selection.kind === 'type') {
    return { ...range, order_type: props.selection.orderType }
  }
  return { ...range, date: props.selection.date }
}

function typeLabel(orderType: OrderType): string {
  return t(`payment.statistics.types.${orderType}`)
}

function formatAmount(amount: number): string {
  return formatPaymentAmount(amount, 'CNY', locale.value)
}

function formatPaidAt(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(parsed)
}

function handleClose(): void {
  requestGeneration++
  emit('close')
}
</script>
