<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-4 sm:p-6">
        <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
          <div><h2 class="text-lg font-semibold">{{ t('admin.workbench.galleryTitle') }}</h2><p class="mt-1 text-xs text-gray-500">{{ t('admin.workbench.galleryBrowseHint') }}</p></div>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="load"><Icon name="refresh" class="mr-1.5" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}</button>
        </div>
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="search">
          <div class="w-full sm:w-44"><Input id="gallery-user" v-model="filters.user" :label="t('admin.workbench.filters.user')" :placeholder="t('admin.workbench.filters.userPlaceholder')" :error="filterError" /></div>
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
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Input from '@/components/common/Input.vue'
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
const filters = reactive({ user: '', task: '' })
const items = ref<WorkbenchGalleryTask[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const error = ref(false)
const filterError = ref('')
let controller: AbortController | null = null
let sequence = 0
const formatTime = (value: string) => new Date(value).toLocaleString(locale.value)
async function load() {
  let userID: number | undefined
  filterError.value = ''
  try { userID = workbenchUserIdFilter(filters.user) } catch { filterError.value = t('admin.workbench.filters.invalidUser'); return }
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
  page.value = 1
  const query = { ...(filters.user.trim() ? { user_id: filters.user.trim() } : {}), ...(filters.task.trim() ? { task_id: filters.task.trim() } : {}) }
  if (JSON.stringify(route.query) === JSON.stringify(query)) void load()
  else void router.replace({ query })
}
function reset() { filters.user = ''; filters.task = ''; search() }
function setPage(value: number) { page.value = value; void load() }
function setPageSize(value: number) { pageSize.value = Math.min(value, 50); page.value = 1; void load() }
watch(() => route.query, () => { filters.user = typeof route.query.user_id === 'string' ? route.query.user_id : ''; filters.task = typeof route.query.task_id === 'string' ? route.query.task_id : ''; page.value = 1; void load() }, { immediate: true })
onBeforeUnmount(() => { sequence++; controller?.abort() })
</script>
