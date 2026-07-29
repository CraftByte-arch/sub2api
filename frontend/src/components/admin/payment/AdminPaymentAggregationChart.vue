<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
    <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          {{ t('payment.admin.aggregation.timelineTitle') }}
        </h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t(`payment.admin.aggregation.granularity.${granularity}`) }}
        </p>
      </div>
      <span class="text-xs text-gray-400 dark:text-gray-500">
        {{ t('payment.admin.aggregation.chartAmountHint') }}
      </span>
    </div>

    <div class="h-72">
      <div v-if="loading" class="flex h-full items-center justify-center">
        <LoadingSpinner size="md" />
      </div>
      <Line v-else-if="chartData" :data="chartData" :options="chartOptions" />
      <div v-else class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('payment.admin.aggregation.noData') }}
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
} from 'chart.js'
import { Line } from 'vue-chartjs'
import type {
  AdminPaymentAggregationBucket,
  AdminPaymentAggregationGranularity,
} from '@/types/payment'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const props = defineProps<{
  data: AdminPaymentAggregationBucket[]
  granularity: AdminPaymentAggregationGranularity
  loading?: boolean
}>()

const { t } = useI18n()

const seriesColors = [
  ['rgb(37, 99, 235)', 'rgba(37, 99, 235, 0.12)'],
  ['rgb(5, 150, 105)', 'rgba(5, 150, 105, 0.12)'],
  ['rgb(217, 119, 6)', 'rgba(217, 119, 6, 0.12)'],
  ['rgb(124, 58, 237)', 'rgba(124, 58, 237, 0.12)'],
]

const currencies = computed(() =>
  [...new Set(props.data.flatMap((bucket) => Object.keys(bucket.amount || {})))].sort(),
)

const chartData = computed(() => {
  if (!props.data.length || !currencies.value.length) return null

  return {
    labels: props.data.map((bucket) => bucket.period),
    datasets: [
      ...currencies.value.map((currency, index) => {
        const [borderColor, backgroundColor] = seriesColors[index % seriesColors.length]
        return {
          label: `${currency} ${t('payment.admin.aggregation.amount')}`,
          data: props.data.map((bucket) => bucket.amount?.[currency] || 0),
          borderColor,
          backgroundColor,
          fill: true,
          tension: 0.28,
          pointRadius: props.data.length > 45 ? 0 : 3,
          pointHoverRadius: 5,
          yAxisID: 'amount',
        }
      }),
      {
        label: t('payment.admin.aggregation.orderCount'),
        data: props.data.map((bucket) => bucket.order_count),
        borderColor: 'rgb(107, 114, 128)',
        backgroundColor: 'rgba(107, 114, 128, 0.1)',
        fill: false,
        tension: 0.28,
        pointRadius: props.data.length > 45 ? 0 : 2,
        pointHoverRadius: 4,
        yAxisID: 'count',
      },
    ],
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { mode: 'index' as const, intersect: false },
  scales: {
    amount: {
      type: 'linear' as const,
      position: 'left' as const,
      beginAtZero: true,
      grid: { color: 'rgba(148, 163, 184, 0.14)' },
      ticks: { precision: 0 },
      title: { display: true, text: t('payment.admin.aggregation.amount') },
    },
    count: {
      type: 'linear' as const,
      position: 'right' as const,
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      ticks: { precision: 0 },
      title: { display: true, text: t('payment.admin.aggregation.orderCount') },
    },
    x: {
      grid: { display: false },
      ticks: { maxRotation: 0, autoSkip: true, maxTicksLimit: 12 },
    },
  },
  plugins: {
    legend: { position: 'top' as const, align: 'start' as const },
    tooltip: { intersect: false },
  },
}))
</script>
