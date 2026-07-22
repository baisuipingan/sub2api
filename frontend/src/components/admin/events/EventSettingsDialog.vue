<template>
  <BaseDialog :show="show" :title="t('events.settings.title')" width="extra-wide" @close="emit('close')">
    <div class="space-y-5">
      <div class="inline-flex border border-gray-200 p-1 dark:border-dark-700">
        <button type="button" class="px-4 py-2 text-sm" :class="tab === 'categories' ? activeTabClass : inactiveTabClass" @click="tab = 'categories'">
          {{ t('events.settings.categories') }}
        </button>
        <button type="button" class="px-4 py-2 text-sm" :class="tab === 'sources' ? activeTabClass : inactiveTabClass" @click="tab = 'sources'">
          {{ t('events.settings.sources') }}
        </button>
        <button type="button" class="px-4 py-2 text-sm" :class="tab === 'map' ? activeTabClass : inactiveTabClass" @click="tab = 'map'">
          {{ t('events.settings.map') }}
        </button>
      </div>

      <template v-if="tab === 'categories'">
        <form class="grid gap-3 border-b border-gray-200 pb-5 md:grid-cols-[1fr_1.5fr_100px_100px_100px_auto] dark:border-dark-700" @submit.prevent="saveCategory">
          <input v-model.trim="categoryForm.code" class="input" :placeholder="t('events.settings.code')" required maxlength="64" />
          <input v-model.trim="categoryForm.name" class="input" :placeholder="t('events.settings.name')" required maxlength="100" />
          <input v-model.trim="categoryForm.color" type="color" class="input h-10 p-1" />
          <input v-model.number="categoryForm.sort_order" type="number" class="input" :placeholder="t('events.settings.order')" />
          <label class="flex items-center justify-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="categoryForm.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />{{ t('common.enabled') }}
          </label>
          <button class="btn btn-primary" :disabled="busy">{{ categoryForm.id ? t('common.save') : t('common.add') }}</button>
        </form>
        <div class="divide-y divide-gray-200 dark:divide-dark-700">
          <div v-for="category in categories" :key="category.id" class="flex items-center gap-3 py-3">
            <span class="h-4 w-4 flex-none" :style="{ backgroundColor: category.color }" />
            <div class="min-w-0 flex-1">
              <div class="font-medium text-gray-900 dark:text-white">{{ category.name }}</div>
              <div class="text-xs text-gray-500">{{ category.code }}</div>
            </div>
            <span class="badge" :class="category.enabled ? 'badge-success' : 'badge-gray'">{{ category.enabled ? t('common.enabled') : t('common.disabled') }}</span>
            <button type="button" class="btn btn-ghost btn-sm" :title="t('common.edit')" @click="editCategory(category)"><Icon name="edit" size="sm" /></button>
            <button type="button" class="btn btn-ghost btn-sm text-red-600" :title="t('common.delete')" @click="emit('delete-category', category.id)"><Icon name="trash" size="sm" /></button>
          </div>
        </div>
      </template>

      <template v-else-if="tab === 'sources'">
        <form class="grid gap-3 border-b border-gray-200 pb-5 md:grid-cols-[1fr_1.5fr_130px_110px_auto] dark:border-dark-700" @submit.prevent="saveSource">
          <input v-model.trim="sourceForm.code" class="input" :placeholder="t('events.settings.code')" required maxlength="64" />
          <input v-model.trim="sourceForm.name" class="input" :placeholder="t('events.settings.name')" required maxlength="100" />
          <select v-model="sourceForm.kind" class="input">
            <option value="json">JSON</option>
            <option value="crawler">Crawler</option>
            <option value="manual">Manual</option>
          </select>
          <label class="flex items-center justify-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="sourceForm.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />{{ t('common.enabled') }}
          </label>
          <button class="btn btn-primary" :disabled="busy">{{ sourceForm.id ? t('common.save') : t('common.add') }}</button>
          <div class="md:col-span-5">
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.settings.sourceConfig') }}</label>
            <textarea v-model="sourceConfigText" rows="5" maxlength="65536" class="input resize-y font-mono text-xs" spellcheck="false" />
            <p v-if="sourceConfigError" class="mt-1 text-xs text-red-600">{{ sourceConfigError }}</p>
          </div>
        </form>
        <div class="divide-y divide-gray-200 dark:divide-dark-700">
          <div v-for="source in sources" :key="source.id" class="flex items-center gap-3 py-3">
            <div class="min-w-0 flex-1">
              <div class="font-medium text-gray-900 dark:text-white">{{ source.name }}</div>
              <div class="text-xs text-gray-500">{{ source.code }} · {{ source.kind }}</div>
            </div>
            <span class="badge" :class="source.enabled ? 'badge-success' : 'badge-gray'">{{ source.enabled ? t('common.enabled') : t('common.disabled') }}</span>
            <button type="button" class="btn btn-ghost btn-sm" :title="t('common.edit')" @click="editSource(source)"><Icon name="edit" size="sm" /></button>
            <button type="button" class="btn btn-ghost btn-sm text-red-600" :title="t('common.delete')" @click="emit('delete-source', source.id)"><Icon name="trash" size="sm" /></button>
          </div>
        </div>
      </template>

      <form v-else class="space-y-5" @submit.prevent="saveMap">
        <label class="flex items-center gap-3 text-sm font-medium text-gray-800 dark:text-gray-200">
          <input v-model="mapForm.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600" />
          {{ t('events.settings.eventCenterEnabled') }}
        </label>
        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.settings.amapKey') }}</label>
            <input v-model.trim="mapForm.amap_key" class="input" autocomplete="off" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.settings.securityCode') }}</label>
            <input v-model.trim="mapForm.security_code" class="input" autocomplete="off" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.settings.defaultLatitude') }}</label>
            <input v-model.number="mapForm.default_latitude" type="number" min="-90" max="90" step="0.000001" class="input" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.settings.defaultLongitude') }}</label>
            <input v-model.number="mapForm.default_longitude" type="number" min="-180" max="180" step="0.000001" class="input" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.settings.defaultZoom') }}</label>
            <input v-model.number="mapForm.default_zoom" type="number" min="3" max="20" class="input" />
          </div>
        </div>
        <div class="flex justify-end"><button class="btn btn-primary" :disabled="busy">{{ t('common.save') }}</button></div>
      </form>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { EventCategory, EventMapSettings, EventSource } from '@/types'

const props = defineProps<{ show: boolean; categories: EventCategory[]; sources: EventSource[]; mapSettings: EventMapSettings | null; busy: boolean }>()
const emit = defineEmits<{
  (event: 'close'): void
  (event: 'save-category', payload: { id?: number; value: Omit<EventCategory, 'id'> }): void
  (event: 'delete-category', id: number): void
  (event: 'save-source', payload: { id?: number; value: Omit<EventSource, 'id' | 'last_sync_at'> }): void
  (event: 'delete-source', id: number): void
  (event: 'save-map', value: EventMapSettings): void
}>()
const { t } = useI18n()
const tab = ref<'categories' | 'sources' | 'map'>('categories')
const activeTabClass = 'bg-primary-600 text-white'
const inactiveTabClass = 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'

const categoryForm = reactive({ id: undefined as number | undefined, code: '', name: '', color: '#2563EB', sort_order: 0, enabled: true, icon: 'calendar' })
const sourceForm = reactive({ id: undefined as number | undefined, code: '', name: '', kind: 'json' as EventSource['kind'], enabled: true, config: {} as Record<string, unknown> })
const sourceConfigText = ref('{}')
const sourceConfigError = ref('')
const mapForm = reactive<EventMapSettings>({ enabled: true, amap_key: '', security_code: '', default_latitude: 31.2304, default_longitude: 121.4737, default_zoom: 11 })

function resetCategory() {
  Object.assign(categoryForm, { id: undefined, code: '', name: '', color: '#2563EB', sort_order: 0, enabled: true, icon: 'calendar' })
}
function resetSource() {
  Object.assign(sourceForm, { id: undefined, code: '', name: '', kind: 'json', enabled: true, config: {} })
  sourceConfigText.value = '{}'
  sourceConfigError.value = ''
}
function editCategory(value: EventCategory) {
  Object.assign(categoryForm, value)
}
function editSource(value: EventSource) {
  Object.assign(sourceForm, { id: value.id, code: value.code, name: value.name, kind: value.kind, enabled: value.enabled, config: value.config })
  sourceConfigText.value = JSON.stringify(value.config || {}, null, 2)
  sourceConfigError.value = ''
}
function saveCategory() {
  emit('save-category', { id: categoryForm.id, value: { code: categoryForm.code, name: categoryForm.name, color: categoryForm.color, icon: categoryForm.icon, sort_order: categoryForm.sort_order, enabled: categoryForm.enabled } })
  resetCategory()
}
function saveSource() {
  try {
    const parsed: unknown = JSON.parse(sourceConfigText.value || '{}')
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) throw new Error('invalid config')
    sourceForm.config = parsed as Record<string, unknown>
    sourceConfigError.value = ''
  } catch {
    sourceConfigError.value = t('events.settings.invalidSourceConfig')
    return
  }
  emit('save-source', { id: sourceForm.id, value: { code: sourceForm.code, name: sourceForm.name, kind: sourceForm.kind, enabled: sourceForm.enabled, config: sourceForm.config } })
  resetSource()
}
function saveMap() {
  emit('save-map', { ...mapForm })
}

watch(() => props.show, (show) => {
  if (show) {
    resetCategory()
    resetSource()
    if (props.mapSettings) Object.assign(mapForm, props.mapSettings)
  }
})

watch(() => props.mapSettings, (value) => { if (value) Object.assign(mapForm, value) }, { deep: true })
</script>
