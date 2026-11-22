<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters><div class="card p-4 sm:p-6"><header class="mb-4 flex flex-wrap items-start justify-between gap-3"><div><h2 class="text-lg font-semibold">用户文件</h2><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">工作台上传资料、办公成果与云端图库。点击文件后才加载内容。</p></div><button type="button" class="btn btn-secondary" :disabled="loading" @click="load"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />刷新</button></header><form class="flex flex-wrap items-end gap-3" @submit.prevent="search"><div class="w-full sm:w-80"><UserSearchSelect ref="userPicker" id="office-files-user" v-model="userId" label="用户" placeholder="搜索名称、昵称或邮箱" :error="filterError" @change="onUserChange" /></div><button type="submit" class="btn btn-primary" :disabled="loading">搜索</button><button type="button" class="btn btn-secondary" @click="reset">重置</button></form><p v-if="error" role="alert" class="mt-3 text-sm text-red-600">{{ error }}</p></div></template>
      <template #table><DataTable class="workbench-resources-table" :data="items" :columns="columns" :loading="loading" row-key="version_id">
        <template #cell-name="{ row }">
          <div class="min-w-0 md:w-[clamp(11rem,22vw,22rem)]">
            <button type="button" class="flex w-full min-w-0 items-center justify-end gap-2 text-primary-600 hover:underline dark:text-primary-400 md:justify-start" :title="row.name" @click="openPreview(row)">
              <Icon name="document" size="sm" class="shrink-0" />
              <span class="min-w-0 truncate">{{ row.name }}</span>
            </button>
            <p class="mt-1 truncate font-mono text-[11px] text-gray-400" :title="row.id">{{ row.id }}</p>
          </div>
        </template>
        <template #cell-user="{ row }">
          <div class="min-w-0 md:w-[clamp(8rem,14vw,14rem)]">
            <p class="truncate text-sm" :title="row.user_name || row.user_email">{{ row.user_name || row.user_email || `#${row.user_id}` }}</p>
            <p v-if="row.user_name && row.user_email" class="mt-1 truncate text-xs text-gray-500" :title="row.user_email">{{ row.user_email }}</p>
            <p class="text-xs text-gray-400">#{{ row.user_id }}</p>
          </div>
        </template>
        <template #cell-source="{ row }"><p class="text-sm">{{ sourceLabel(row.source) }}</p><p v-if="row.parent_version_id" class="mt-1 text-xs text-gray-500">文件修改版本</p></template>
        <template #cell-bytes="{ row }">
          <p class="whitespace-nowrap text-sm">{{ formatBytes(row.bytes) }}</p>
          <p class="mt-1 text-xs text-gray-500" :title="row.mime">{{ resourceFormat(row) }}</p>
        </template>
        <template #cell-created_at="{ row }"><p class="whitespace-nowrap text-xs">{{ officeTime(row.created_at) }}</p></template>
        <template #cell-actions="{ row }"><button class="btn btn-secondary whitespace-nowrap text-xs" type="button" @click="openPreview(row)">查看文件</button></template>
        <template #empty><EmptyState title="没有匹配的文件" description="用户上传资料或生成办公成果后，将在这里显示。" /></template>
      </DataTable></template>
      <template #pagination><Pagination v-if="total" :total="total" :page="page" :page-size="pageSize" @update:page="setPage" @update:page-size="setPageSize" /></template>
    </TablePageLayout>
    <BaseDialog :show="!!selected" title="查看用户文件" width="extra-wide" @close="selected = null"><p class="mb-3 text-xs text-gray-500">{{ selected?.user_name || selected?.user_email }} · #{{ selected?.user_id }}</p><div v-if="selected" class="bs-chat h-[70dvh] overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600"><ResourcePreview :key="selected.version_id" :file="selected" :files="[selected]" :load-access="loadAccess" read-only hide-fullscreen @close="selected = null" @preview="value => { if (!value) selected = null }" /></div></BaseDialog>
  </AppLayout>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import UserSearchSelect from '@/components/admin/UserSearchSelect.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ResourcePreview from '@/components/workbench/chat/ResourcePreview.vue'
import { getWorkbenchChatResourceAccess, listWorkbenchChatResources, type WorkbenchAdminResource } from '@/api/admin/workbench'
import { formatBytes } from '@/utils/format'
import { officeTime } from './workbenchChatDisplay'
import { chatError } from '@/stores/workbenchChat'
import '@/components/workbench/chat/chat.css'
const items = ref<WorkbenchAdminResource[]>([]), loading = ref(false), total = ref(0), page = ref(1), pageSize = ref(20), error = ref(''), filterError = ref('')
const userId = ref<number>(), appliedUser = ref<number>(), userPicker = ref<InstanceType<typeof UserSearchSelect> | null>(null), selected = ref<WorkbenchAdminResource | null>(null)
let sequence = 0, controller: AbortController | undefined, resetting = false
const columns = [
  { key: 'name', label: '文件' },
  { key: 'user', label: '用户' },
  { key: 'source', label: '来源', class: 'w-28' },
  { key: 'bytes', label: '大小 / 类型', class: 'w-28' },
  { key: 'created_at', label: '创建时间', class: 'w-40' },
  { key: 'actions', label: '内容', class: 'w-28' },
]
const resourceFormats: Record<string, string> = {
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'DOCX',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'XLSX',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'PPTX',
  'application/pdf': 'PDF',
  'application/zip': 'ZIP',
  'application/json': 'JSON',
  'application/vnd.jgraph.mxfile': 'DRAW.IO',
  'image/jpeg': 'JPEG',
  'image/png': 'PNG',
  'image/webp': 'WebP',
  'text/plain': 'TXT',
  'text/markdown': 'MD',
  'text/csv': 'CSV',
  'text/x-python': 'PY',
}
function resourceFormat(file: WorkbenchAdminResource) {
  const mime = file.mime.split(';', 1)[0].trim().toLowerCase()
  const extension = file.name.match(/\.([a-z0-9]{1,10})$/i)?.[1]
  return resourceFormats[mime] || extension?.toUpperCase() || '—'
}
async function load() { controller?.abort(); controller = new AbortController(); const own = ++sequence; loading.value = true; error.value = ''; try { const value = await listWorkbenchChatResources({ page: page.value, page_size: pageSize.value, ...(appliedUser.value ? { user_id: appliedUser.value } : {}) }, controller.signal); if (own !== sequence) return; items.value = value.items; total.value = value.total; if (value.pages > 0 && page.value > value.pages) { page.value = value.pages; void load() } } catch (e) { if (own === sequence) { error.value = chatError(e); items.value = []; total.value = 0 } } finally { if (own === sequence) loading.value = false } }
function search() { filterError.value = ''; if (userPicker.value?.hasUnselectedQuery()) { filterError.value = '请从搜索结果中选择用户'; return } appliedUser.value = userId.value; page.value = 1; selected.value = null; void load() }
function onUserChange() { if (!resetting) search() }
function reset() { resetting = true; userId.value = undefined; userPicker.value?.clear(); resetting = false; search() }
function setPage(value: number) { page.value = value; void load() }
function setPageSize(value: number) { pageSize.value = value; page.value = 1; void load() }
function sourceLabel(value?: string) { return ({ upload: '用户上传', generated: '办公生成', gallery: '云端图库' } as Record<string, string>)[value || ''] || value || '—' }
function openPreview(file: WorkbenchAdminResource) { selected.value = file }
const loadAccess = (version: string) => getWorkbenchChatResourceAccess(version)
onMounted(() => { void load() })
onBeforeUnmount(() => { ++sequence; controller?.abort() })
</script>

<style scoped>
.workbench-resources-table :deep([data-field] > span:first-child) {
  flex-shrink: 0;
  white-space: nowrap;
}
</style>
