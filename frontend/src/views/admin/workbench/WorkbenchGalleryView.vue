<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-4 sm:p-6">
        <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
          <div><h2 class="text-lg font-semibold">{{ t('admin.workbench.galleryTitle') }}</h2><p class="mt-1 text-xs text-gray-500">{{ t('admin.workbench.galleryBrowseHint') }}</p></div>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load"><Icon name="refresh" class="mr-1.5" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}</button>
        </div>
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="search">
          <div class="w-full sm:w-64"><UserSearchSelect ref="userPicker" id="gallery-user" v-model="selectedUserId" :label="t('admin.workbench.filters.user')" :placeholder="t('admin.workbench.filters.userPlaceholder')" :error="filterError" @change="onUserChange" /></div>
          <div class="min-w-0 flex-1 basis-48"><Input id="gallery-task" v-model="filters.task" :label="t('admin.workbench.galleryTaskFilter')" :placeholder="t('admin.workbench.galleryTaskPlaceholder')" /></div>
          <button type="submit" class="btn btn-primary" :disabled="loading">{{ t('common.search') }}</button>
          <button type="button" class="btn btn-secondary" @click="reset">{{ t('common.reset') }}</button>
        </form>
        <p v-if="error" role="alert" class="mt-3 text-sm text-red-600">{{ t('admin.workbench.loadFailed') }}</p>
      </section>
      <LoadingSpinner v-if="loading" class="mx-auto" />
      <div v-else-if="items.length" class="grid gap-5 lg:grid-cols-2 2xl:grid-cols-3">
        <article v-for="task in items" :key="`${task.user_id}:${task.task_id}`" class="card min-w-0 p-4">
          <header class="mb-3 space-y-1 text-xs">
            <p class="break-words font-medium">{{ task.user_email || t('admin.workbench.user', { id: task.user_id }) }} <span class="text-gray-500">#{{ task.user_id }}</span></p>
            <p class="break-all font-mono text-gray-500">{{ task.task_id }}</p>
            <div class="flex flex-wrap justify-between gap-1 text-gray-500"><span class="break-words">{{ task.model || '—' }}</span><time :datetime="task.created_at">{{ formatTime(task.created_at) }}</time></div>
          </header>
          <div class="grid grid-cols-3 gap-2 sm:grid-cols-4">
            <WorkbenchGalleryImage v-for="(id, index) in task.image_ids" :key="`${task.user_id}:${id}`" :task="task" :image-id="id" :number="index + 1" />
          </div>
          <p class="mt-3 text-xs text-gray-500">{{ t('admin.workbench.galleryResultCount', { count: task.output_count }) }}</p>
        </article>
      </div>
      <EmptyState v-else :title="t('admin.workbench.galleryEmpty')" :description="t('admin.workbench.galleryEmptyHint')" />
      <Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" :page-size-options="[10, 20, 50]" @update:page="setPage" @update:page-size="setPageSize" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Input from '@/components/common/Input.vue'
import UserSearchSelect from '@/components/admin/UserSearchSelect.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { listWorkbenchGallery, type WorkbenchGalleryTask } from '@/api/admin/workbench'
import { workbenchUserIdFilter } from './workbenchDisplay'
import WorkbenchGalleryImage from './WorkbenchGalleryImage.vue'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const filters = reactive({ task: '' })
const selectedUserId = ref<number>()
const userPicker = ref<InstanceType<typeof UserSearchSelect> | null>(null)
let resettingFilters = false
function onUserChange() { if (!resettingFilters) search() }
const items = ref<WorkbenchGalleryTask[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const error = ref(false)
const filterError = ref('')
let controller: AbortController | null = null
let sequence = 0
let routeSequence = 0
const formatTime = (value: string) => new Date(value).toLocaleString(locale.value)
async function load() {
  if (userPicker.value?.hasUnselectedQuery()) { filterError.value = t('admin.workbench.filters.chooseUser'); return }
  const userID = selectedUserId.value
  controller?.abort()
  controller = new AbortController()
  const current = ++sequence
  loading.value = true
  error.value = false
  // Unmount old thumbnails rather than carrying signed links across filters.
  items.value = []
  try {
    const result = await listWorkbenchGallery({ page: page.value, page_size: Math.min(pageSize.value, 50), ...(userID ? { user_id: userID } : {}), ...(filters.task.trim() ? { task_id: filters.task.trim() } : {}) }, controller.signal)
    if (current !== sequence) return
    items.value = result.items
    total.value = result.total
    if (result.pages > 0 && page.value > result.pages) { page.value = result.pages; void load() }
  } catch { if (current === sequence) { error.value = true; total.value = 0 } }
  finally { if (current === sequence) loading.value = false }
}
function search() {
  filterError.value = ''
  if (userPicker.value?.hasUnselectedQuery()) { filterError.value = t('admin.workbench.filters.chooseUser'); return }
  page.value = 1
  const query = { ...(selectedUserId.value ? { user_id: String(selectedUserId.value) } : {}), ...(filters.task.trim() ? { task_id: filters.task.trim() } : {}) }
  if (JSON.stringify(route.query) === JSON.stringify(query)) void load()
  else void router.replace({ query })
}
function reset() {
  resettingFilters = true
  filters.task = ''; selectedUserId.value = undefined
  userPicker.value?.clear()
  resettingFilters = false
  search()
}
function setPage(value: number) { page.value = value; void load() }
function setPageSize(value: number) { pageSize.value = Math.min(value, 50); page.value = 1; void load() }
watch(() => route.query, async () => {
  const revision = ++routeSequence
  filterError.value = ''
  try {
    selectedUserId.value = workbenchUserIdFilter(typeof route.query.user_id === 'string' ? route.query.user_id : '')
  } catch {
    selectedUserId.value = undefined
    filterError.value = t('admin.workbench.filters.invalidUser')
    controller?.abort()
    sequence++
    loading.value = false
    items.value = []
    total.value = 0
    return
  }
  if (selectedUserId.value === undefined) {
    resettingFilters = true
    userPicker.value?.clear()
    resettingFilters = false
  }
  filters.task = typeof route.query.task_id === 'string' ? route.query.task_id : ''
  page.value = 1
  await nextTick()
  if (revision === routeSequence) void load()
}, { immediate: true })
onBeforeUnmount(() => { sequence++; routeSequence++; controller?.abort() })
</script>
