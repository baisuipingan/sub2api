<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <input v-model="search" class="input min-w-52 flex-1 sm:max-w-64" :placeholder="t('events.admin.search')" @input="scheduleSearch" />
          <select v-model="filters.status" class="input w-40" @change="reloadFromFirstPage">
            <option value="">{{ t('events.admin.allStatuses') }}</option>
            <option value="draft">{{ t('events.status.draft') }}</option>
            <option value="published">{{ t('events.status.published') }}</option>
            <option value="cancelled">{{ t('events.status.cancelled') }}</option>
            <option value="archived">{{ t('events.status.archived') }}</option>
          </select>
          <select v-model="filters.category" class="input w-40" @change="reloadFromFirstPage">
            <option value="">{{ t('events.admin.allCategories') }}</option>
            <option v-for="category in categories" :key="category.id" :value="category.code">{{ category.name }}</option>
          </select>
          <div class="ml-auto flex flex-wrap gap-2">
            <button class="btn btn-secondary" :title="t('common.refresh')" :disabled="loading" @click="loadEvents"><Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" /></button>
            <button class="btn btn-secondary" @click="showSettings = true"><Icon name="cog" size="md" class="mr-1" />{{ t('events.settings.title') }}</button>
            <button class="btn btn-secondary" @click="showImport = true"><Icon name="upload" size="md" class="mr-1" />{{ t('events.import.title') }}</button>
            <button class="btn btn-primary" @click="openCreate"><Icon name="plus" size="md" class="mr-1" />{{ t('events.admin.create') }}</button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="events" :loading="loading" :server-side-sort="true" default-sort-key="created_at" default-sort-order="desc" @sort="handleSort">
          <template #cell-title="{ row }">
            <div class="max-w-md">
              <div class="truncate font-medium text-gray-900 dark:text-white">{{ row.title }}</div>
              <div class="mt-1 truncate text-xs text-gray-500">{{ row.summary || row.organizer_name || `#${row.id}` }}</div>
            </div>
          </template>
          <template #cell-status="{ row }">
            <span class="badge" :class="statusBadge(row.status)">{{ t(`events.status.${row.status}`) }}</span>
          </template>
          <template #cell-category="{ row }">
            <span v-if="row.category" class="inline-flex items-center gap-2 text-sm"><span class="h-2.5 w-2.5" :style="{ backgroundColor: row.category.color }" />{{ row.category.name }}</span>
            <span v-else class="text-sm text-gray-400">-</span>
          </template>
          <template #cell-schedule="{ row }">
            <div v-if="row.occurrences[0]" class="text-sm">
              <div class="font-medium text-gray-800 dark:text-gray-200">{{ formatDateTime(row.occurrences[0].starts_at) }}</div>
              <div v-if="row.occurrences.length > 1" class="text-xs text-gray-500">{{ t('events.admin.occurrenceCount', { count: row.occurrences.length }) }}</div>
            </div>
          </template>
          <template #cell-location="{ row }">
            <div v-if="row.occurrences[0]" class="max-w-56 text-sm text-gray-600 dark:text-gray-300">
              <div class="truncate">{{ row.occurrences[0].venue_name || t(`events.location.${row.occurrences[0].location_mode}`) }}</div>
              <div class="truncate text-xs text-gray-500">{{ [row.occurrences[0].city, row.occurrences[0].district].filter(Boolean).join(' ') }}</div>
            </div>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button class="btn btn-ghost btn-sm" :title="t('common.edit')" @click="openEdit(row)"><Icon name="edit" size="sm" /></button>
              <button v-if="row.status === 'draft'" class="btn btn-ghost btn-sm text-emerald-600" :title="t('events.actions.publish')" @click="changeStatus(row, 'published')"><Icon name="check" size="sm" /></button>
              <button v-if="row.status === 'published'" class="btn btn-ghost btn-sm text-amber-600" :title="t('events.actions.cancel')" @click="openCancel(row)"><Icon name="ban" size="sm" /></button>
              <button v-if="row.status !== 'archived'" class="btn btn-ghost btn-sm" :title="t('events.actions.archive')" @click="changeStatus(row, 'archived')"><Icon name="archive" size="sm" /></button>
              <button class="btn btn-ghost btn-sm text-red-600" :title="t('common.delete')" @click="confirmDelete(row)"><Icon name="trash" size="sm" /></button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="changePage" @update:page-size="changePageSize" />
      </template>
    </TablePageLayout>

    <EventEditorDialog
      :show="showEditor"
      :event="editingEvent"
      :categories="categories"
      :groups="groups"
      :map-settings="mapSettings"
      :saving="saving"
      @close="showEditor = false"
      @save="saveEvent"
    />
    <EventImportDialog :show="showImport" :sources="sources" @close="showImport = false" @imported="handleImported" />
    <EventSettingsDialog
      :show="showSettings"
      :categories="categories"
      :sources="sources"
      :map-settings="mapSettings"
      :busy="settingsBusy"
      @close="showSettings = false"
      @save-category="saveCategory"
      @delete-category="deleteCategory"
      @save-source="saveSource"
      @delete-source="deleteSource"
      @save-map="saveMapSettings"
    />

    <BaseDialog :show="showCancelDialog" :title="t('events.actions.cancel')" width="narrow" @close="showCancelDialog = false">
      <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.cancelledReason') }}</label>
      <textarea v-model.trim="cancelReason" rows="4" maxlength="1000" class="input resize-y" />
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="showCancelDialog = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-danger" :disabled="!cancelReason || saving" @click="submitCancel">{{ t('events.actions.confirmCancel') }}</button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog :show="showDeleteDialog" :title="t('events.admin.deleteTitle')" :message="t('events.admin.deleteMessage', { title: deletingEvent?.title || '' })" danger @confirm="deleteEvent" @cancel="showDeleteDialog = false" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import type { Column } from '@/components/common/types'
import type { AdminGroup, EventCategory, EventMapSettings, EventSource, EventStatus, EventWriteRequest, TeamEvent } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import EventEditorDialog from '@/components/admin/events/EventEditorDialog.vue'
import EventImportDialog from '@/components/admin/events/EventImportDialog.vue'
import EventSettingsDialog from '@/components/admin/events/EventSettingsDialog.vue'

const { t } = useI18n()
const appStore = useAppStore()
const events = ref<TeamEvent[]>([])
const categories = ref<EventCategory[]>([])
const sources = ref<EventSource[]>([])
const groups = ref<AdminGroup[]>([])
const mapSettings = ref<EventMapSettings | null>(null)
const loading = ref(false)
const saving = ref(false)
const settingsBusy = ref(false)
const search = ref('')
const filters = reactive({ status: '', category: '' })
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0, pages: 0 })
const sort = reactive({ sort_by: 'created_at', sort_order: 'desc' as 'asc' | 'desc' })
const showEditor = ref(false)
const showImport = ref(false)
const showSettings = ref(false)
const editingEvent = ref<TeamEvent | null>(null)
const showDeleteDialog = ref(false)
const deletingEvent = ref<TeamEvent | null>(null)
const showCancelDialog = ref(false)
const cancellingEvent = ref<TeamEvent | null>(null)
const cancelReason = ref('')
let controller: AbortController | null = null
let searchTimer: number | null = null

const columns = computed<Column[]>(() => [
  { key: 'title', label: t('events.fields.title'), sortable: true },
  { key: 'status', label: t('events.fields.status'), sortable: true },
  { key: 'category', label: t('events.fields.category') },
  { key: 'schedule', label: t('events.fields.schedule') },
  { key: 'location', label: t('events.fields.location') },
  { key: 'actions', label: t('common.actions') },
])

async function loadEvents() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  try {
    const result = await adminAPI.events.list(pagination.page, pagination.page_size, {
      status: filters.status || undefined,
      category: filters.category || undefined,
      search: search.value || undefined,
      sort_by: sort.sort_by,
      sort_order: sort.sort_order,
    }, request.signal)
    if (controller !== request) return
    events.value = result.items
    Object.assign(pagination, { page: result.page, page_size: result.page_size, total: result.total, pages: result.pages })
  } catch (error: any) {
    if (error?.code !== 'ERR_CANCELED') appStore.showError(error?.message || t('events.admin.loadFailed'))
  } finally {
    if (controller === request) {
      controller = null
      loading.value = false
    }
  }
}

async function loadMetadata() {
  try {
    const [loadedCategories, loadedSources, loadedMapSettings, loadedGroups] = await Promise.all([
      adminAPI.events.listCategories(),
      adminAPI.events.listSources(),
      adminAPI.events.getMapSettings(),
      adminAPI.groups.getAllIncludingInactive(),
    ])
    categories.value = loadedCategories
    sources.value = loadedSources
    mapSettings.value = loadedMapSettings
    groups.value = loadedGroups
  } catch (error: any) {
    appStore.showError(error?.message || t('events.admin.metadataFailed'))
  }
}

function openCreate() {
  editingEvent.value = null
  showEditor.value = true
}
function openEdit(value: TeamEvent) {
  editingEvent.value = value
  showEditor.value = true
}
async function saveEvent(payload: EventWriteRequest) {
  saving.value = true
  try {
    if (editingEvent.value) await adminAPI.events.update(editingEvent.value.id, payload)
    else await adminAPI.events.create(payload)
    appStore.showSuccess(t('events.admin.saved'))
    showEditor.value = false
    await loadEvents()
  } catch (error: any) {
    appStore.showError(error?.message || t('events.admin.saveFailed'))
  } finally {
    saving.value = false
  }
}
async function changeStatus(value: TeamEvent, status: Exclude<EventStatus, 'draft'>, reason = '') {
  saving.value = true
  try {
    await adminAPI.events.setStatus(value.id, status, reason)
    await loadEvents()
  } catch (error: any) {
    appStore.showError(error?.message || t('events.admin.statusFailed'))
  } finally {
    saving.value = false
  }
}
function openCancel(value: TeamEvent) {
  cancellingEvent.value = value
  cancelReason.value = ''
  showCancelDialog.value = true
}
async function submitCancel() {
  if (!cancellingEvent.value || !cancelReason.value) return
  await changeStatus(cancellingEvent.value, 'cancelled', cancelReason.value)
  showCancelDialog.value = false
}
function confirmDelete(value: TeamEvent) {
  deletingEvent.value = value
  showDeleteDialog.value = true
}
async function deleteEvent() {
  if (!deletingEvent.value) return
  try {
    await adminAPI.events.remove(deletingEvent.value.id)
    showDeleteDialog.value = false
    await loadEvents()
  } catch (error: any) {
    appStore.showError(error?.message || t('events.admin.deleteFailed'))
  }
}
async function saveCategory(payload: { id?: number; value: Omit<EventCategory, 'id'> }) {
  settingsBusy.value = true
  try {
    if (payload.id) await adminAPI.events.updateCategory(payload.id, payload.value)
    else await adminAPI.events.createCategory(payload.value)
    await loadMetadata()
    appStore.showSuccess(t('events.settings.saved'))
  } catch (error: any) {
    appStore.showError(error?.message || t('events.settings.categorySaveFailed'))
  } finally { settingsBusy.value = false }
}
async function deleteCategory(id: number) {
  settingsBusy.value = true
  try {
    await adminAPI.events.deleteCategory(id)
    await loadMetadata()
  } catch (error: any) {
    appStore.showError(error?.message || t('events.settings.categoryDeleteFailed'))
  } finally { settingsBusy.value = false }
}
async function saveSource(payload: { id?: number; value: Omit<EventSource, 'id' | 'last_sync_at'> }) {
  settingsBusy.value = true
  try {
    if (payload.id) await adminAPI.events.updateSource(payload.id, payload.value)
    else await adminAPI.events.createSource(payload.value)
    await loadMetadata()
    appStore.showSuccess(t('events.settings.saved'))
  } catch (error: any) {
    appStore.showError(error?.message || t('events.settings.sourceSaveFailed'))
  } finally { settingsBusy.value = false }
}
async function deleteSource(id: number) {
  settingsBusy.value = true
  try {
    await adminAPI.events.deleteSource(id)
    await loadMetadata()
  } catch (error: any) {
    appStore.showError(error?.message || t('events.settings.sourceDeleteFailed'))
  } finally { settingsBusy.value = false }
}
async function saveMapSettings(value: EventMapSettings) {
  settingsBusy.value = true
  try {
    mapSettings.value = await adminAPI.events.updateMapSettings(value)
    await appStore.fetchPublicSettings(true)
    appStore.showSuccess(t('events.settings.saved'))
  } catch (error: any) {
    appStore.showError(error?.message || t('events.settings.saveFailed'))
  } finally { settingsBusy.value = false }
}
function handleImported() { void loadEvents() }
function handleSort(key: string, order: 'asc' | 'desc') { Object.assign(sort, { sort_by: key, sort_order: order }); pagination.page = 1; void loadEvents() }
function changePage(page: number) { pagination.page = page; void loadEvents() }
function changePageSize(size: number) { pagination.page_size = size; pagination.page = 1; void loadEvents() }
function reloadFromFirstPage() { pagination.page = 1; void loadEvents() }
function scheduleSearch() {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reloadFromFirstPage, 300)
}
function statusBadge(status: EventStatus) {
  if (status === 'published') return 'badge-success'
  if (status === 'cancelled') return 'badge-danger'
  if (status === 'archived') return 'badge-warning'
  return 'badge-gray'
}

onMounted(() => { void Promise.all([loadEvents(), loadMetadata()]) })
onUnmounted(() => { controller?.abort(); if (searchTimer) window.clearTimeout(searchTimer) })
</script>
