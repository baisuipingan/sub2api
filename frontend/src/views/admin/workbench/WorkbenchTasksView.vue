<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="card p-4 sm:p-6">
          <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.tasksTitle') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.scope') }}</p>
            </div>
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="load"><Icon name="refresh" class="mr-1.5" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}</button>
          </div>
          <form class="flex flex-wrap items-end gap-3" @submit.prevent="search">
            <div class="w-full sm:w-44"><label for="workbench-status" class="input-label">{{ t('admin.workbench.filters.status') }}</label><Select id="workbench-status" v-model="filters.status" :options="statusOptions" :aria-label="t('admin.workbench.filters.status')" /></div>
            <div class="w-full sm:w-36"><Input id="workbench-user" v-model="filters.user" :label="t('admin.workbench.filters.user')" :placeholder="t('admin.workbench.filters.userPlaceholder')" :error="filterError" /></div>
            <div class="min-w-0 flex-1 basis-48"><Input id="workbench-model" v-model="filters.model" :label="t('admin.workbench.filters.model')" :placeholder="t('admin.workbench.filters.modelPlaceholder')" /></div>
            <button type="submit" class="btn btn-primary" :disabled="loading">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-secondary" @click="reset">{{ t('common.reset') }}</button>
          </form>
          <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbench.metadataHint') }}</p>
          <p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ t('admin.workbench.loadFailed') }}</p>
        </div>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="items" :loading="loading" :row-key="taskRowKey">
          <template #cell-task_id="{ row }">
            <div class="max-w-64 break-words text-xs"><p class="font-mono text-gray-900 dark:text-white">{{ row.task_id }}</p><p class="mt-1 text-gray-500 dark:text-dark-400">{{ row.user_email || t('admin.workbench.user', { id: row.user_id }) }}</p><p class="text-gray-400">#{{ row.user_id }}</p></div>
          </template>
          <template #cell-model="{ row }"><div class="max-w-52 break-words"><p class="font-medium">{{ row.model }}</p><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ row.protocol }}</p></div></template>
          <template #cell-route_calls="{ row }"><WorkbenchTaskRoutes :calls="row.route_calls || []" /></template>
          <template #cell-status="{ row }"><StatusBadge :status="workbenchStatusVariant(row.status)" :label="t('admin.workbench.statuses.' + row.status)" /></template>
          <template #cell-output_count="{ row }"><div class="whitespace-nowrap text-xs"><p>{{ t('admin.workbench.imageProgress', { done: row.output_count, total: row.image_count }) }}</p><p class="mt-1 text-gray-500 dark:text-dark-400">{{ t('admin.workbench.callCount', { count: row.call_count }) }}</p></div></template>
          <template #cell-gallery="{ row }"><RouterLink v-if="row.output_count > 0" :to="{ path: '/admin/workbench/gallery', query: { user_id: String(row.user_id), task_id: row.task_id } }" class="btn btn-secondary whitespace-nowrap text-xs">{{ t('admin.workbench.galleryViewResults') }}</RouterLink><span v-else class="text-gray-400">—</span></template>
          <template #cell-created_at="{ row }"><div class="whitespace-nowrap text-xs"><time :datetime="row.created_at">{{ formatTime(row.created_at) }}</time><p v-if="workbenchElapsedSeconds(row) !== null" class="mt-1 text-gray-500 dark:text-dark-400">{{ t(row.finished_at ? 'admin.workbench.duration' : 'admin.workbench.runningDuration', { seconds: workbenchElapsedSeconds(row) }) }}</p></div></template>
          <template #cell-error_code="{ row }"><div class="max-w-60 space-y-1 break-words text-xs"><p v-if="row.error_code" class="font-mono">{{ row.error_code }}</p><template v-if="row.error_message"><p class="whitespace-pre-wrap">{{ errorReason(row).summary }}</p><details v-if="errorReason(row).details"><summary class="cursor-pointer text-primary-600">{{ t('imageStudio.errors.details') }}</summary><p class="mt-1 whitespace-pre-wrap">{{ errorReason(row).details }}</p></details></template><p v-if="row.status === 'unknown'" class="text-amber-700 dark:text-amber-400">{{ t('admin.workbench.unknownHint') }}</p><p v-if="row.billing_pending" class="text-gray-500 dark:text-dark-400">{{ t('admin.workbench.billingPendingCount', { count: row.billing_pending }) }}</p><p v-if="row.billing_unknown" class="text-amber-700 dark:text-amber-400">{{ t('admin.workbench.billingUnknownCount', { count: row.billing_unknown }) }}</p><span v-if="!row.error_code && row.status !== 'unknown' && !row.billing_pending && !row.billing_unknown">—</span></div></template>
          <template #empty><EmptyState :title="t(hasFilters ? 'admin.workbench.noMatches' : 'admin.workbench.emptyTasks')" :description="t('admin.workbench.emptyHint')" :action-text="error ? t('common.refresh') : undefined" :action-icon="false" @action="load" /></template>
        </DataTable>
      </template>
      <template #pagination><Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" @update:page="setPage" @update:page-size="setPageSize" /></template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import WorkbenchTaskRoutes from './WorkbenchTaskRoutes.vue'
import { listWorkbenchTasks, WORKBENCH_TASK_STATUSES, type WorkbenchTask, type WorkbenchTaskQuery, type WorkbenchTaskStatus } from '@/api/admin/workbench'
import { getImageErrorPresentation } from '@/features/imageStudio/lib/apiErrors'
import { workbenchElapsedSeconds, workbenchStatusVariant, workbenchUserIdFilter } from './workbenchDisplay'

const { t, locale } = useI18n()
const filters = reactive({ status: '', user: '', model: '' })
const applied = ref<WorkbenchTaskQuery>({})
const items = ref<WorkbenchTask[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const error = ref(false)
const filterError = ref('')
let controller: AbortController | null = null
let sequence = 0
const statusOptions = computed(() => [{ value: '', label: t('admin.workbench.filters.all') }, ...WORKBENCH_TASK_STATUSES.map(value => ({ value, label: t('admin.workbench.statuses.' + value) }))])
const columns = computed(() => [
  { key: 'task_id', label: t('admin.workbench.columns.task') },
  { key: 'model', label: t('admin.workbench.columns.model') },
  { key: 'route_calls', label: t('admin.workbench.columns.upstream') },
  { key: 'status', label: t('admin.workbench.columns.status') },
  { key: 'output_count', label: t('admin.workbench.columns.images') },
  { key: 'gallery', label: t('admin.workbench.galleryResults') },
  { key: 'created_at', label: t('admin.workbench.columns.time') },
  { key: 'error_code', label: t('admin.workbench.columns.diagnostic') },
])
const hasFilters = computed(() => Object.keys(applied.value).length > 0)
const errorReason = (task: WorkbenchTask) => {
  const detail = task.error_message || task.error_code
  return getImageErrorPresentation(task.error_code && !detail.includes(`[${task.error_code}]`) ? `[${task.error_code}] ${detail}` : detail, t)
}
const formatTime = (value: string) => new Date(value).toLocaleString(locale.value)
// Public task IDs are unique within a user, not across the administrator view.
const taskRowKey = (task: WorkbenchTask) => `${task.user_id}:${task.task_id}`

async function load() {
  controller?.abort()
  controller = new AbortController()
  const current = ++sequence
  loading.value = true
  error.value = false
  try {
    const result = await listWorkbenchTasks({ ...applied.value, page: page.value, page_size: pageSize.value }, controller.signal)
    if (current !== sequence) return
    items.value = result.items
    total.value = result.total
    // A concurrent deletion can invalidate a final page; request the last
    // valid page without reusing or replaying any generation request.
    if (result.pages > 0 && page.value > result.pages) { page.value = result.pages; void load() }
  } catch {
    if (current === sequence) { error.value = true; items.value = []; total.value = 0 }
  } finally {
    if (current === sequence) loading.value = false
  }
}

function search() {
  filterError.value = ''
  let userId: number | undefined
  try { userId = workbenchUserIdFilter(filters.user) } catch { filterError.value = t('admin.workbench.filters.invalidUser'); return }
  applied.value = {
    ...(filters.status ? { status: filters.status as WorkbenchTaskStatus } : {}),
    ...(userId ? { user_id: userId } : {}),
    ...(filters.model.trim() ? { model: filters.model.trim() } : {}),
  }
  page.value = 1
  void load()
}
function reset() { filters.status = ''; filters.user = ''; filters.model = ''; search() }
function setPage(value: number) { page.value = value; void load() }
function setPageSize(value: number) { pageSize.value = value; page.value = 1; void load() }
onMounted(() => { void load() })
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
