<template>
  <BaseDialog :show="show" :title="t('events.import.title')" width="extra-wide" @close="close">
    <div class="space-y-6">
      <div
        class="flex min-h-40 cursor-pointer flex-col items-center justify-center border-2 border-dashed border-gray-300 px-6 py-8 text-center transition-colors hover:border-primary-400 dark:border-dark-600"
        @click="fileInput?.click()"
        @dragover.prevent
        @drop.prevent="handleDrop"
      >
        <Icon name="upload" size="xl" class="mb-3 text-gray-400" />
        <p class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ selectedFile?.name || t('events.import.chooseFile') }}</p>
        <p class="mt-1 text-xs text-gray-500">{{ t('events.import.fileHint') }}</p>
        <input ref="fileInput" type="file" accept="application/json,.json" class="hidden" @change="handleFileChange" />
      </div>

      <div v-if="parsedEnvelope" class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.import.source') }}</label>
          <select v-model="selectedSource" class="input">
            <option v-for="source in enabledSources" :key="source.id" :value="source.code">{{ source.name }}</option>
          </select>
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.import.mode') }}</label>
          <select v-model="mode" class="input">
            <option value="upsert">{{ t('events.import.upsert') }}</option>
            <option value="create_only">{{ t('events.import.createOnly') }}</option>
          </select>
        </div>
      </div>

      <div v-if="batch" class="border-y border-gray-200 py-4 dark:border-dark-700">
        <div class="grid grid-cols-2 gap-y-4 sm:grid-cols-3 lg:grid-cols-6">
          <div v-for="stat in stats" :key="stat.label" class="px-3">
            <div class="text-xs text-gray-500">{{ stat.label }}</div>
            <div class="mt-1 text-xl font-semibold" :class="stat.className">{{ stat.value }}</div>
          </div>
        </div>
      </div>

      <div v-if="batch?.items.length" class="max-h-80 overflow-auto border border-gray-200 dark:border-dark-700">
        <table class="w-full text-left text-sm">
          <thead class="sticky top-0 bg-gray-50 text-xs text-gray-500 dark:bg-dark-800">
            <tr>
              <th class="px-3 py-2">#</th>
              <th class="px-3 py-2">External ID</th>
              <th class="px-3 py-2">{{ t('events.import.action') }}</th>
              <th class="px-3 py-2">{{ t('events.import.result') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
            <tr v-for="item in batch.items" :key="item.id">
              <td class="px-3 py-2 text-gray-500">{{ item.item_index + 1 }}</td>
              <td class="max-w-48 truncate px-3 py-2">{{ item.external_id || '-' }}</td>
              <td class="px-3 py-2"><span class="badge" :class="actionBadge(item.action)">{{ actionLabel(item.action) }}</span></td>
              <td class="max-w-md px-3 py-2 text-gray-600 dark:text-gray-300">{{ item.error_detail || item.status }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <label v-if="batch?.status === 'previewed'" class="flex items-center gap-3 text-sm text-gray-700 dark:text-gray-300">
        <input v-model="publishAfterImport" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />
        {{ t('events.import.publishAfterImport') }}
      </label>
    </div>

    <template #footer>
      <div class="flex flex-wrap justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="downloadTemplate">
          <Icon name="download" size="sm" class="mr-1" />{{ t('events.import.downloadTemplate') }}
        </button>
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.close') }}</button>
        <button v-if="parsedEnvelope && !batch" type="button" class="btn btn-primary" :disabled="busy || !selectedSource" @click="preview">
          <Icon v-if="busy" name="refresh" size="sm" class="mr-1 animate-spin" />{{ t('events.import.preview') }}
        </button>
        <button v-if="batch?.status === 'previewed'" type="button" class="btn btn-primary" :disabled="busy || batch.create_count + batch.update_count === 0" @click="commit">
          <Icon v-if="busy" name="refresh" size="sm" class="mr-1 animate-spin" />{{ t('events.import.commit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { EventImportBatch, EventImportEnvelope, EventSource } from '@/types'

const props = defineProps<{ show: boolean; sources: EventSource[] }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'imported'): void }>()
const { t } = useI18n()
const appStore = useAppStore()

const fileInput = ref<HTMLInputElement | null>(null)
const selectedFile = ref<File | null>(null)
const parsedEnvelope = ref<EventImportEnvelope | null>(null)
const selectedSource = ref('')
const mode = ref<'create_only' | 'upsert'>('upsert')
const batch = ref<EventImportBatch | null>(null)
const busy = ref(false)
const publishAfterImport = ref(false)

const enabledSources = computed(() => props.sources.filter((source) => source.enabled && source.kind !== 'manual'))
const stats = computed(() => batch.value ? [
  { label: t('events.import.total'), value: batch.value.total_count, className: 'text-gray-900 dark:text-white' },
  { label: t('events.import.create'), value: batch.value.create_count, className: 'text-emerald-600' },
  { label: t('events.import.update'), value: batch.value.update_count, className: 'text-blue-600' },
  { label: t('events.import.unchanged'), value: batch.value.unchanged_count, className: 'text-gray-500' },
  { label: t('events.import.conflict'), value: batch.value.conflict_count, className: 'text-amber-600' },
  { label: t('events.import.error'), value: batch.value.error_count, className: 'text-red-600' },
] : [])

async function selectFile(file: File | undefined) {
  if (!file || (!file.name.toLowerCase().endsWith('.json') && file.type !== 'application/json')) {
    appStore.showError(t('events.import.invalidFile'))
    return
  }
  if (file.size > 5 * 1024 * 1024) {
    appStore.showError(t('events.import.fileTooLarge'))
    return
  }
  try {
    const parsed = JSON.parse(await file.text()) as EventImportEnvelope
    if (parsed.type !== 'sub2api-events' || parsed.version !== 1 || !Array.isArray(parsed.events) || parsed.events.length === 0 || parsed.events.length > 1000) {
      throw new Error('invalid schema')
    }
    selectedFile.value = file
    parsedEnvelope.value = parsed
    selectedSource.value = enabledSources.value.some((source) => source.code === parsed.source)
      ? parsed.source
      : enabledSources.value[0]?.code || ''
    mode.value = parsed.mode || 'upsert'
    batch.value = null
  } catch {
    appStore.showError(t('events.import.invalidFormat'))
  }
}

function handleFileChange(event: Event) {
  void selectFile((event.target as HTMLInputElement).files?.[0])
}

function handleDrop(event: DragEvent) {
  void selectFile(event.dataTransfer?.files?.[0])
}

async function preview() {
  if (!parsedEnvelope.value || !selectedSource.value) return
  busy.value = true
  try {
    batch.value = await adminAPI.events.previewImport({
      ...parsedEnvelope.value,
      source: selectedSource.value,
      file_name: selectedFile.value?.name,
      mode: mode.value,
    })
  } catch (error: any) {
    appStore.showError(error?.message || t('events.import.previewFailed'))
  } finally {
    busy.value = false
  }
}

async function commit() {
  if (!batch.value) return
  busy.value = true
  try {
    batch.value = await adminAPI.events.commitImport(batch.value.id, publishAfterImport.value)
    appStore.showSuccess(t('events.import.committed'))
    emit('imported')
  } catch (error: any) {
    appStore.showError(error?.message || t('events.import.commitFailed'))
  } finally {
    busy.value = false
  }
}

function downloadTemplate() {
  const payload = {
    type: 'sub2api-events',
    version: 1,
    source: selectedSource.value || enabledSources.value[0]?.code || 'json',
    mode: 'upsert',
    defaults: {
      timezone: 'Asia/Shanghai',
      coordinate_system: 'wgs84',
      country: '中国',
      province: '上海市',
      city: '上海',
    },
    events: [{
      external_id: 'source-event-001',
      source_url: 'https://example.com/events/source-event-001',
      category: 'meetup',
      title: 'AI 开发者交流活动',
      summary: '活动摘要',
      description_markdown: '## 活动介绍\n\n填写 Markdown 活动详情。',
      tags: ['AI', '开发者'],
      organizer: { name: '主办方名称', url: 'https://example.com' },
      fee: { type: 'free', price_min: null, price_max: null, currency: 'CNY' },
      registration_url: 'https://example.com/events/source-event-001/register',
      visibility: 'authenticated',
      audience: {},
      occurrences: [{
        starts_at: '2026-08-01T06:00:00Z',
        ends_at: '2026-08-01T09:00:00Z',
        location_mode: 'offline',
        venue_name: '活动场地',
        address: '详细地址',
        district: '浦东新区',
        latitude: 31.2304,
        longitude: 121.4737,
      }],
    }],
  }
  saveAs(new Blob([`${JSON.stringify(payload, null, 2)}\n`], { type: 'application/json;charset=utf-8' }), 'sub2api-events-template.json')
}

function actionBadge(action: string) {
  if (action === 'create') return 'badge-success'
  if (action === 'update') return 'badge-primary'
  if (action === 'conflict') return 'badge-warning'
  if (action === 'error') return 'badge-danger'
  return 'badge-gray'
}

function actionLabel(action: string) {
  return t(`events.import.actions.${action}`)
}

function reset() {
  selectedFile.value = null
  parsedEnvelope.value = null
  selectedSource.value = enabledSources.value[0]?.code || ''
  mode.value = 'upsert'
  batch.value = null
  busy.value = false
  publishAfterImport.value = false
  if (fileInput.value) fileInput.value.value = ''
}

function close() {
  if (!busy.value) emit('close')
}

watch(() => props.show, (show) => { if (show) reset() })
</script>
