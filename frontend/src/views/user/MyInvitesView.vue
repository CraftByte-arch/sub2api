<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex items-center justify-end">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadInvitees"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="invitees" :loading="loading" row-key="user_id">
          <template #cell-email="{ value }">
            <span class="block break-all font-medium text-gray-900 dark:text-white">
              {{ value || '-' }}
            </span>
          </template>
          <template #cell-username="{ value }">
            <span class="block break-all text-gray-700 dark:text-gray-300">{{ value || '-' }}</span>
          </template>
          <template #cell-total_rebate="{ value }">
            <span class="font-medium text-emerald-600 dark:text-emerald-400">
              {{ formatCurrency(value) }}
            </span>
          </template>
          <template #cell-created_at="{ value }">
            <span class="text-gray-700 dark:text-gray-300">{{ formatDateTime(value) || '-' }}</span>
          </template>
          <template #empty>
            <div class="flex flex-col items-center py-2 text-center">
              <Icon
                :name="loadFailed ? 'exclamationCircle' : 'inbox'"
                size="xl"
                class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500"
              />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">
                {{ loadFailed ? t('myInvites.loadFailed') : t('myInvites.empty') }}
              </p>
              <button
                v-if="loadFailed"
                type="button"
                class="btn btn-secondary btn-sm mt-4"
                :disabled="loading"
                @click="loadInvitees"
              >
                <Icon name="refresh" size="sm" />
                <span>{{ t('common.refresh') }}</span>
              </button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import userAPI from '@/api/user'
import type { Column } from '@/components/common/types'
import type { AffiliateInvitee } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency, formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const loadFailed = ref(false)
const invitees = ref<AffiliateInvitee[]>([])
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const columns = computed<Column[]>(() => [
  { key: 'email', label: t('myInvites.columns.email') },
  { key: 'username', label: t('myInvites.columns.username') },
  { key: 'total_rebate', label: t('myInvites.columns.totalRebate') },
  { key: 'created_at', label: t('myInvites.columns.joinedAt') },
])

async function loadInvitees(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  try {
    const response = await userAPI.getMyInvitees({
      page: pagination.page,
      page_size: pagination.page_size,
    })
    invitees.value = response.items || []
    pagination.total = response.total || 0
    pagination.page = response.page || pagination.page
    pagination.page_size = response.page_size || pagination.page_size
  } catch (error) {
    loadFailed.value = true
    appStore.showError(extractApiErrorMessage(error, t('myInvites.loadFailed')))
  } finally {
    loading.value = false
  }
}

function handlePageChange(page: number): void {
  pagination.page = page
  void loadInvitees()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadInvitees()
}

onMounted(() => {
  void loadInvitees()
})
</script>
