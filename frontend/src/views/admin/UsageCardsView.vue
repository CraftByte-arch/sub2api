<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-end">
        <button class="btn btn-primary" @click="openPlanDialog(emptyPlan())">{{ t('admin.usageCards.addPlan') }}</button>
      </div>

      <div class="card p-5">
        <h2 class="mb-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.usageCards.plans') }}</h2>
        <div class="overflow-x-auto">
          <table class="min-w-full text-sm">
            <thead class="text-left text-gray-500 dark:text-gray-400">
              <tr>
                <th class="py-2">{{ t('admin.usageCards.sortOrder') }}</th>
                <th class="py-2">{{ t('admin.usageCards.name') }}</th>
                <th>{{ t('admin.usageCards.price') }}</th>
                <th>{{ t('admin.usageCards.amount') }}</th>
                <th>{{ t('admin.usageCards.validity') }}</th>
                <th>{{ t('admin.usageCards.forSale') }}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="plan in plans" :key="plan.id" class="border-t border-gray-100 dark:border-dark-700">
                <td class="py-3">
                  <div class="flex items-center gap-2">
                    <span class="inline-flex min-w-[3rem] items-center justify-center rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-300">
                      {{ plan.sort_order }}
                    </span>
                    <div class="flex items-center gap-1">
                      <button
                        type="button"
                        class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-gray-200 text-gray-500 transition hover:border-primary-300 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-40 dark:border-dark-600 dark:text-gray-300 dark:hover:border-primary-600 dark:hover:text-primary-300"
                        :title="t('admin.usageCards.moveUp')"
                        :disabled="isPlanOrderUpdating(plan.id) || !canMovePlan(plan.id, -1)"
                        @click="movePlan(plan.id, -1)"
                      >
                        <Icon name="arrowUp" size="sm" />
                      </button>
                      <button
                        type="button"
                        class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-gray-200 text-gray-500 transition hover:border-primary-300 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-40 dark:border-dark-600 dark:text-gray-300 dark:hover:border-primary-600 dark:hover:text-primary-300"
                        :title="t('admin.usageCards.moveDown')"
                        :disabled="isPlanOrderUpdating(plan.id) || !canMovePlan(plan.id, 1)"
                        @click="movePlan(plan.id, 1)"
                      >
                        <Icon name="arrowDown" size="sm" />
                      </button>
                    </div>
                  </div>
                </td>
                <td class="py-3">{{ plan.name }}</td>
                <td>{{ plan.price }}</td>
                <td>${{ plan.amount_usd }}</td>
                <td>{{ t('admin.usageCards.days', { days: plan.validity_days }) }}</td>
                <td>
                  <button
                    type="button"
                    class="inline-flex min-w-[88px] items-center justify-center rounded-full px-3 py-1 text-xs font-medium transition disabled:cursor-not-allowed disabled:opacity-60"
                    :class="plan.for_sale
                      ? 'bg-emerald-50 text-emerald-700 hover:bg-emerald-100 dark:bg-emerald-900/20 dark:text-emerald-300 dark:hover:bg-emerald-900/30'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600'"
                    :disabled="isPlanSaleUpdating(plan.id)"
                    :title="plan.for_sale ? t('admin.usageCards.saleOnTitle') : t('admin.usageCards.saleOffTitle')"
                    @click="togglePlanForSale(plan)"
                  >
                    {{ isPlanSaleUpdating(plan.id) ? t('admin.usageCards.updating') : plan.for_sale ? t('admin.usageCards.onSale') : t('admin.usageCards.offSale') }}
                  </button>
                </td>
                <td class="space-x-2 text-right">
                  <button class="btn btn-sm btn-secondary" @click="openPlanDialog(plan)">{{ t('common.edit') }}</button>
                  <button class="btn btn-sm btn-danger" @click="deletePlan(plan.id)">{{ t('common.delete') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.usageCards.userCards') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.userCardsHint') }}</p>
          </div>
          <div class="flex flex-1 flex-wrap items-center justify-end gap-3">
            <div class="relative w-full sm:w-72" data-filter-user-search>
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
              />
              <input
                v-model="filterUserKeyword"
                type="text"
                :placeholder="t('admin.usageCards.searchUserPlaceholder')"
                class="input pl-10 pr-8"
                @input="debounceSearchFilterUsers"
                @focus="showFilterUserDropdown = true"
              />
              <button
                v-if="selectedFilterUser"
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                :title="t('common.clear')"
                @click="clearFilterUser"
              >
                <Icon name="x" size="sm" :stroke-width="2" />
              </button>

              <div
                v-if="showFilterUserDropdown && (filterUserResults.length > 0 || filterUserKeyword)"
                class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-gray-700 dark:bg-gray-800"
              >
                <div
                  v-if="filterUserLoading"
                  class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('common.loading') }}
                </div>
                <div
                  v-else-if="filterUserResults.length === 0 && filterUserKeyword"
                  class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('common.noOptionsFound') }}
                </div>
                <button
                  v-for="user in filterUserResults"
                  :key="user.id"
                  type="button"
                  class="w-full px-4 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700"
                  @click="selectFilterUser(user)"
                >
                  <span class="font-medium text-gray-900 dark:text-white">{{ user.email }}</span>
                  <span v-if="user.username" class="ml-2 text-gray-500 dark:text-gray-400">{{ user.username }}</span>
                  <span class="ml-2 text-gray-400 dark:text-gray-500">#{{ user.id }}</span>
                </button>
              </div>
            </div>

            <div class="w-full sm:w-40">
              <Select
                v-model="filters.status"
                :options="statusOptions"
                :placeholder="t('admin.usageCards.allStatus')"
                @change="loadCards"
              />
            </div>

            <button
              class="btn btn-secondary"
              :disabled="loadingCards"
              :title="t('common.refresh')"
              @click="loadCards"
            >
              <Icon name="refresh" size="md" :class="loadingCards ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>

        <div class="card overflow-hidden">
          <DataTable
            :columns="cardColumns"
            :data="cards"
            :loading="loadingCards"
            :sticky-first-column="true"
            :sticky-actions-column="true"
            row-key="id"
          >
            <template #cell-user="{ row }">
              <div class="flex items-center gap-2">
                <div class="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
                  <span class="text-sm font-medium text-primary-700 dark:text-primary-300">
                    {{ userInitial(row) }}
                  </span>
                </div>
                <div class="min-w-0">
                  <div class="truncate font-medium text-gray-900 dark:text-white">
                    {{ row.user?.email || t('admin.redeem.userPrefix', { id: row.user_id }) }}
                  </div>
                  <div class="truncate text-xs text-gray-500 dark:text-gray-400">
                    {{ row.user?.username || t('admin.usageCards.noUsername') }}
                  </div>
                </div>
              </div>
            </template>

            <template #cell-name="{ row }">
              <div class="font-medium text-gray-900 dark:text-white">{{ row.name }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ sourceLabel(row.source) }}</div>
            </template>

            <template #cell-usage="{ row }">
              <div class="min-w-[240px] space-y-2">
                <div class="flex items-center gap-2">
                  <div class="h-1.5 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="progressClass(row)"
                      :style="{ width: progressWidth(row) }"
                    ></div>
                  </div>
                  <span class="whitespace-nowrap text-xs font-medium text-gray-700 dark:text-gray-300">
                    {{ usagePercent(row) }}%
                  </span>
                </div>
                <div class="flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t('admin.usageCards.used') }} ${{ row.used_usd.toFixed(4) }}</span>
                  <span>{{ t('admin.usageCards.remaining') }} ${{ row.remaining_usd.toFixed(4) }}</span>
                  <span>{{ t('admin.usageCards.total') }} ${{ row.total_limit_usd.toFixed(2) }}</span>
                </div>
              </div>
            </template>

            <template #cell-status="{ row }">
              <span
                class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium"
                :class="statusBadgeClass(effectiveCardStatus(row))"
              >
                {{ usageCardStatusLabel(effectiveCardStatus(row)) }}
              </span>
            </template>

            <template #cell-expires_at="{ row }">
              <div class="whitespace-nowrap text-sm text-gray-900 dark:text-white">{{ formatDateTime(row.expires_at) }}</div>
            </template>

            <template #cell-actions="{ row }">
              <div class="flex justify-end gap-2">
                <button v-if="effectiveCardStatus(row) === 'active'" class="btn btn-sm btn-secondary" @click="suspendCard(row.id)">{{ t('admin.usageCards.suspend') }}</button>
                <button v-if="effectiveCardStatus(row) === 'suspended'" class="btn btn-sm btn-secondary" @click="resumeCard(row.id)">{{ t('admin.usageCards.resume') }}</button>
                <button v-if="canRevokeCard(effectiveCardStatus(row))" class="btn btn-sm btn-danger" @click="cancelCard(row.id)">{{ t('admin.usageCards.cancel') }}</button>
                <span v-if="!hasCardActions(effectiveCardStatus(row))" class="text-sm text-gray-400 dark:text-gray-500">{{ t('admin.usageCards.noActions') }}</span>
              </div>
            </template>
          </DataTable>
        </div>
      </div>

      <BaseDialog
        :show="editingPlan !== null"
        :title="editingPlan?.id ? t('admin.usageCards.editPlan') : t('admin.usageCards.newPlan')"
        width="wide"
        @close="closePlanDialog"
      >
        <form id="usage-card-plan-form" class="space-y-5" @submit.prevent="savePlan">
          <div class="rounded-lg border border-primary-100 bg-primary-50/70 p-3 text-sm text-primary-800 dark:border-primary-900/40 dark:bg-primary-900/20 dark:text-primary-200">
            {{ t('admin.usageCards.description') }}
          </div>

          <div>
            <label class="input-label">{{ t('admin.usageCards.planName') }} <span class="text-red-500">*</span></label>
            <input
              v-model.trim="planForm.name"
              class="input"
              maxlength="100"
              :placeholder="t('admin.usageCards.planNamePlaceholder')"
              required
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.planNameHint') }}</p>
          </div>

          <div>
            <label class="input-label">{{ t('admin.usageCards.productName') }}</label>
            <input
              v-model.trim="planForm.product_name"
              class="input"
              maxlength="100"
              :placeholder="t('admin.usageCards.productNamePlaceholder')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.productNameHint') }}</p>
          </div>

          <div>
            <label class="input-label">{{ t('admin.usageCards.planDescription') }}</label>
            <textarea
              v-model.trim="planForm.description"
              class="input"
              rows="3"
              :placeholder="t('admin.usageCards.planDescriptionPlaceholder')"
            ></textarea>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.planDescriptionHint') }}</p>
          </div>

          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.usageCards.price') }} <span class="text-red-500">*</span></label>
              <div class="relative">
                <input v-model.number="planForm.price" class="input pr-16" type="number" min="0.01" step="0.01" required />
                <span class="pointer-events-none absolute right-4 top-1/2 -translate-y-1/2 text-sm text-gray-400">{{ t('admin.usageCards.paymentAmount') }}</span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.priceHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('admin.usageCards.availableAmount') }} <span class="text-red-500">*</span></label>
              <div class="relative">
                <input v-model.number="planForm.amount_usd" class="input pl-8 pr-20" type="number" min="0.0001" step="0.0001" required />
                <span class="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-sm text-gray-400">$</span>
                <span class="pointer-events-none absolute right-4 top-1/2 -translate-y-1/2 text-sm text-gray-400">{{ t('admin.usageCards.billingUsage') }}</span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.amountHint') }}</p>
            </div>
          </div>

          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.usageCards.validityDays') }} <span class="text-red-500">*</span></label>
              <div class="relative">
                <input v-model.number="planForm.validity_days" class="input pr-12" type="number" min="1" step="1" required />
                <span class="pointer-events-none absolute right-4 top-1/2 -translate-y-1/2 text-sm text-gray-400">{{ t('admin.usageCards.dayUnit') }}</span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.validityHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('admin.usageCards.sortOrder') }}</label>
              <input v-model.number="planForm.sort_order" class="input" type="number" min="0" step="1" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.sortHint') }}</p>
            </div>
          </div>

          <div>
            <label class="input-label">{{ t('admin.usageCards.features') }}</label>
            <textarea
              v-model.trim="planForm.features"
              class="input"
              rows="2"
              :placeholder="t('admin.usageCards.featuresPlaceholder')"
            ></textarea>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.featuresHint') }}</p>
          </div>

          <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <input v-model="planForm.for_sale" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.usageCards.saleEnabled') }}</span>
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageCards.saleEnabledHint') }}</span>
            </span>
          </label>

          <p v-if="planError" class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ planError }}</p>
        </form>

        <template #footer>
          <div class="flex justify-end gap-3">
            <button type="button" class="btn btn-secondary" @click="closePlanDialog">{{ t('common.cancel') }}</button>
            <button type="submit" form="usage-card-plan-form" class="btn btn-primary" :disabled="savingPlan">
              {{ savingPlan ? t('admin.usageCards.saving') : t('common.save') }}
            </button>
          </div>
        </template>
      </BaseDialog>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { SimpleUser } from '@/api/admin/usage'
import { adminUsageCardsAPI, type UserUsageCard } from '@/api/usageCards'
import type { UsageCardPlan } from '@/types/payment'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useI18n } from 'vue-i18n'

const plans = ref<UsageCardPlan[]>([])
const cards = ref<UserUsageCard[]>([])
const editingPlan = ref<Partial<UsageCardPlan> | null>(null)
const savingPlan = ref(false)
const loadingCards = ref(false)
const planError = ref('')
const updatingPlanSaleIds = ref(new Set<number>())
const updatingPlanOrderIds = ref(new Set<number>())
const appStore = useAppStore()
const { t } = useI18n()

const filters = reactive({
  user_id: undefined as number | undefined,
  status: 'active'
})

const filterUserKeyword = ref('')
const selectedFilterUser = ref<SimpleUser | null>(null)
const filterUserResults = ref<SimpleUser[]>([])
const filterUserLoading = ref(false)
const showFilterUserDropdown = ref(false)
let filterUserSearchTimeout: ReturnType<typeof setTimeout> | null = null

const statusOptions = computed(() => [
  { value: '', label: t('admin.usageCards.allStatus') },
  { value: 'active', label: t('usageCards.status.active') },
  { value: 'exhausted', label: t('usageCards.status.exhausted') },
  { value: 'expired', label: t('usageCards.status.expired') },
  { value: 'suspended', label: t('usageCards.status.suspended') },
  { value: 'cancelled', label: t('usageCards.status.cancelled') },
])

const cardColumns = computed(() => [
  { key: 'user', label: t('admin.usageCards.user'), sortable: false },
  { key: 'name', label: t('admin.usageCards.name'), sortable: true },
  { key: 'usage', label: t('admin.usageCards.remainingTotal'), sortable: false },
  { key: 'status', label: t('admin.usageCards.status'), sortable: true },
  { key: 'expires_at', label: t('admin.usageCards.expiresAt'), sortable: true },
  { key: 'actions', label: t('admin.usageCards.actions'), sortable: false },
])

const planForm = reactive({
  id: undefined as number | undefined,
  name: '',
  description: '',
  product_name: '',
  price: 10,
  amount_usd: 10,
  validity_days: 30,
  features: '',
  for_sale: true,
  sort_order: 0,
})

function emptyPlan(): Partial<UsageCardPlan> {
  return { name: '', description: '', product_name: '', price: 10, amount_usd: 10, validity_days: 30, features: '', for_sale: true, sort_order: 0 }
}

function openPlanDialog(plan: Partial<UsageCardPlan>) {
  planForm.id = plan.id
  planForm.name = plan.name ?? ''
  planForm.description = plan.description ?? ''
  planForm.product_name = plan.product_name ?? ''
  planForm.price = plan.price ?? 10
  planForm.amount_usd = plan.amount_usd ?? 10
  planForm.validity_days = plan.validity_days ?? 30
  planForm.features = plan.features ?? ''
  planForm.for_sale = plan.for_sale ?? true
  planForm.sort_order = plan.sort_order ?? 0
  planError.value = ''
  editingPlan.value = { ...plan }
}

function closePlanDialog() {
  editingPlan.value = null
  planError.value = ''
}

function validatePlan() {
  if (!planForm.name.trim()) return t('admin.usageCards.validation.nameRequired')
  if (planForm.product_name.trim().length > 100) return t('admin.usageCards.validation.productNameMaxLength')
  if (!Number.isFinite(planForm.price) || planForm.price <= 0) return t('admin.usageCards.validation.pricePositive')
  if (!Number.isFinite(planForm.amount_usd) || planForm.amount_usd <= 0) return t('admin.usageCards.validation.amountPositive')
  if (!Number.isInteger(planForm.validity_days) || planForm.validity_days <= 0) return t('admin.usageCards.validation.validityPositiveInteger')
  if (!Number.isInteger(planForm.sort_order) || planForm.sort_order < 0) return t('admin.usageCards.validation.sortNonNegativeInteger')
  return ''
}

function usageCardStatusLabel(status: string) {
  return t(`usageCards.status.${status}`, status)
}

function effectiveCardStatus(card: UserUsageCard) {
  if (card.status === 'active') {
    const expiresAt = new Date(card.expires_at).getTime()
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) return 'expired'
    if (card.total_limit_usd > 0 && card.used_usd >= card.total_limit_usd) return 'exhausted'
  }
  return card.status
}

function canRevokeCard(status: string) {
  return status === 'active' || status === 'suspended'
}

function hasCardActions(status: string) {
  return status === 'active' || status === 'suspended'
}

function statusBadgeClass(status: string) {
  switch (status) {
    case 'active':
      return 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300'
    case 'expired':
    case 'exhausted':
      return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300'
    case 'suspended':
      return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
    case 'cancelled':
      return 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
  }
}

function usagePercent(card: UserUsageCard) {
  if (!card.total_limit_usd || card.total_limit_usd <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((card.used_usd / card.total_limit_usd) * 100)))
}

function progressWidth(card: UserUsageCard) {
  return `${usagePercent(card)}%`
}

function progressClass(card: UserUsageCard) {
  const percent = usagePercent(card)
  if (percent >= 90) return 'bg-red-500'
  if (percent >= 70) return 'bg-yellow-500'
  return 'bg-green-500'
}

function userInitial(card: UserUsageCard) {
  return (card.user?.email || card.user?.username || String(card.user_id) || '?').charAt(0).toUpperCase()
}

function sourceLabel(source: string) {
  return t(`usageCards.source.${source}`, source)
}

function formatDateTime(value: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

async function loadPlans() {
  const res = await adminUsageCardsAPI.listPlans()
  plans.value = res.data
}

async function loadCards() {
  loadingCards.value = true
  try {
    const params: { user_id?: number; status?: string } = {}
    if (filters.user_id) params.user_id = filters.user_id
    if (filters.status) params.status = filters.status
    const res = await adminUsageCardsAPI.listCards(params)
    cards.value = res.data
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err) || t('admin.usageCards.failedToLoadCards'))
  } finally {
    loadingCards.value = false
  }
}

async function load() {
  await Promise.all([loadPlans(), loadCards()])
}

function debounceSearchFilterUsers() {
  if (filterUserSearchTimeout) clearTimeout(filterUserSearchTimeout)
  filterUserSearchTimeout = setTimeout(searchFilterUsers, 300)
}

async function searchFilterUsers() {
  const keyword = filterUserKeyword.value.trim()
  if (!keyword) {
    filterUserResults.value = []
    return
  }
  filterUserLoading.value = true
  try {
    filterUserResults.value = await adminAPI.usage.searchUsers(keyword)
  } catch (err: unknown) {
    console.error('Failed to search users:', err)
    filterUserResults.value = []
  } finally {
    filterUserLoading.value = false
  }
}

function selectFilterUser(user: SimpleUser) {
  selectedFilterUser.value = user
  filters.user_id = user.id
  filterUserKeyword.value = user.username ? `${user.email} / ${user.username}` : user.email
  showFilterUserDropdown.value = false
  void loadCards()
}

function clearFilterUser() {
  selectedFilterUser.value = null
  filters.user_id = undefined
  filterUserKeyword.value = ''
  filterUserResults.value = []
  void loadCards()
}

async function savePlan() {
  if (!editingPlan.value) return
  planError.value = validatePlan()
  if (planError.value) return
  savingPlan.value = true
  const payload = {
    name: planForm.name.trim(),
    description: planForm.description.trim(),
    product_name: planForm.product_name.trim(),
    price: planForm.price,
    amount_usd: planForm.amount_usd,
    validity_days: planForm.validity_days,
    features: planForm.features.trim(),
    for_sale: planForm.for_sale,
    sort_order: planForm.sort_order,
  }
  try {
    if (planForm.id) await adminUsageCardsAPI.updatePlan(planForm.id, payload)
    else await adminUsageCardsAPI.createPlan(payload)
    appStore.showSuccess(t('admin.usageCards.saved'))
    closePlanDialog()
    await Promise.all([loadPlans(), loadCards()])
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err) || t('admin.usageCards.saveFailed'))
  } finally {
    savingPlan.value = false
  }
}

function isPlanSaleUpdating(id: number) {
  return updatingPlanSaleIds.value.has(id)
}

function markPlanSaleUpdating(id: number, updating: boolean) {
  const next = new Set(updatingPlanSaleIds.value)
  if (updating) next.add(id)
  else next.delete(id)
  updatingPlanSaleIds.value = next
}

function isPlanOrderUpdating(id: number) {
  return updatingPlanOrderIds.value.has(id)
}

function markPlanOrderUpdating(ids: number[], updating: boolean) {
  const next = new Set(updatingPlanOrderIds.value)
  for (const id of ids) {
    if (updating) next.add(id)
    else next.delete(id)
  }
  updatingPlanOrderIds.value = next
}

function canMovePlan(id: number, direction: -1 | 1) {
  const index = plans.value.findIndex((item) => item.id === id)
  if (index < 0) return false
  const targetIndex = index + direction
  return targetIndex >= 0 && targetIndex < plans.value.length
}

async function movePlan(id: number, direction: -1 | 1) {
  const index = plans.value.findIndex((item) => item.id === id)
  const targetIndex = index + direction
  if (index < 0 || targetIndex < 0 || targetIndex >= plans.value.length) return

  const reordered = [...plans.value]
  const [moved] = reordered.splice(index, 1)
  reordered.splice(targetIndex, 0, moved)

  const updates = reordered
    .map((plan, orderIndex) => ({
      plan,
      sort_order: orderIndex,
    }))
    .filter(({ plan, sort_order }) => plan.sort_order !== sort_order)

  if (updates.length === 0) return

  const affectedIds = updates.map(({ plan }) => plan.id)
  markPlanOrderUpdating(affectedIds, true)
  try {
    await Promise.all(
      updates.map(({ plan, sort_order }) =>
        adminUsageCardsAPI.updatePlan(plan.id, {
          name: plan.name,
          description: plan.description,
          price: plan.price,
          amount_usd: plan.amount_usd,
          validity_days: plan.validity_days,
          features: plan.features,
          for_sale: plan.for_sale,
          sort_order,
        }),
      ),
    )
    plans.value = reordered.map((plan, orderIndex) => ({ ...plan, sort_order: orderIndex }))
    appStore.showSuccess(t('admin.usageCards.orderUpdated'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err) || t('admin.usageCards.updateFailed'))
    await loadPlans()
  } finally {
    markPlanOrderUpdating(affectedIds, false)
  }
}

async function togglePlanForSale(plan: UsageCardPlan) {
  if (isPlanSaleUpdating(plan.id)) return
  markPlanSaleUpdating(plan.id, true)
  try {
    const payload = {
      name: plan.name,
      description: plan.description,
      price: plan.price,
      amount_usd: plan.amount_usd,
      validity_days: plan.validity_days,
      features: plan.features,
      for_sale: !plan.for_sale,
      sort_order: plan.sort_order,
    }
    const res = await adminUsageCardsAPI.updatePlan(plan.id, payload)
    const index = plans.value.findIndex((item) => item.id === plan.id)
    if (index >= 0) plans.value[index] = res.data
    appStore.showSuccess(res.data.for_sale ? t('admin.usageCards.saleEnabledSuccess') : t('admin.usageCards.saleDisabledSuccess'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err) || t('admin.usageCards.updateFailed'))
  } finally {
    markPlanSaleUpdating(plan.id, false)
  }
}

async function deletePlan(id: number) {
  await adminUsageCardsAPI.deletePlan(id)
  await loadPlans()
}

async function cancelCard(id: number) { await adminUsageCardsAPI.cancelCard(id); await loadCards() }
async function suspendCard(id: number) { await adminUsageCardsAPI.suspendCard(id); await loadCards() }
async function resumeCard(id: number) { await adminUsageCardsAPI.resumeCard(id); await loadCards() }

onMounted(load)
</script>
