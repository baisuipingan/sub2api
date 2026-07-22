<template>
  <BaseDialog
    :show="show"
    :title="event ? t('events.admin.edit') : t('events.admin.create')"
    width="extra-wide"
    @close="close"
  >
    <form id="event-editor-form" class="space-y-7" @submit.prevent="submit">
      <section class="grid gap-4 md:grid-cols-2">
        <div class="md:col-span-2">
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.title') }}</label>
          <input v-model.trim="form.title" class="input" maxlength="200" required />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.category') }}</label>
          <select v-model="form.category_id" class="input">
            <option value="">{{ t('events.common.uncategorized') }}</option>
            <option
              v-for="category in categories"
              :key="category.id"
              :value="String(category.id)"
              :disabled="!category.enabled && form.category_id !== String(category.id)"
            >
              {{ category.name }}{{ category.enabled ? '' : ` (${t('common.disabled')})` }}
            </option>
          </select>
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.status') }}</label>
          <select v-model="form.status" class="input">
            <option value="draft">{{ t('events.status.draft') }}</option>
            <option value="published">{{ t('events.status.published') }}</option>
            <option value="cancelled">{{ t('events.status.cancelled') }}</option>
            <option value="archived">{{ t('events.status.archived') }}</option>
          </select>
        </div>
        <div v-if="form.status === 'cancelled'" class="md:col-span-2">
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.cancelledReason') }}</label>
          <textarea v-model.trim="form.cancelled_reason" rows="2" maxlength="1000" class="input resize-y" required />
        </div>
        <div class="md:col-span-2">
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.summary') }}</label>
          <textarea v-model.trim="form.summary" rows="2" maxlength="1000" class="input resize-y" />
        </div>
        <div class="md:col-span-2">
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.description') }}</label>
          <textarea v-model="form.description_markdown" rows="7" maxlength="50000" class="input resize-y font-mono text-sm" />
        </div>
        <div class="md:col-span-2">
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.fields.tags') }}</label>
          <input v-model="form.tags" class="input" :placeholder="t('events.fields.tagsPlaceholder')" />
        </div>
      </section>

      <section class="border-t border-gray-200 pt-6 dark:border-dark-700">
        <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('events.sections.visibility') }}</h3>
        <div class="grid gap-4 md:grid-cols-3">
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.visibility') }}</label>
            <select v-model="form.visibility" class="input">
              <option value="authenticated">{{ t('events.visibility.authenticated') }}</option>
              <option value="targeted">{{ t('events.visibility.targeted') }}</option>
            </select>
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.visibleFrom') }}</label>
            <input v-model="form.visible_from" type="datetime-local" class="input" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.visibleUntil') }}</label>
            <input v-model="form.visible_until" type="datetime-local" class="input" />
          </div>
          <div v-if="form.visibility === 'targeted'" class="md:col-span-3">
            <label class="mb-2 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.audienceGroups') }}</label>
            <div class="grid max-h-52 gap-2 overflow-y-auto border border-gray-200 p-3 sm:grid-cols-2 dark:border-dark-700">
              <label v-for="group in audienceGroups" :key="group.id" class="flex min-w-0 items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input
                  v-model="form.subscription_group_ids"
                  type="checkbox"
                  :value="group.id"
                  :disabled="group.status !== 'active' && !form.subscription_group_ids.includes(group.id)"
                  class="h-4 w-4 flex-none border-gray-300 text-primary-600"
                />
                <span class="min-w-0 flex-1 truncate">{{ group.name }}</span>
                <span class="flex-none text-xs text-gray-400">{{ group.platform }}</span>
              </label>
              <p v-if="audienceGroups.length === 0" class="text-sm text-gray-500">{{ t('events.visibility.noGroups') }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="border-t border-gray-200 pt-6 dark:border-dark-700">
        <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('events.sections.organizer') }}</h3>
        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.organizerName') }}</label>
            <input v-model.trim="form.organizer_name" class="input" maxlength="200" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.organizerUrl') }}</label>
            <input v-model.trim="form.organizer_url" type="url" class="input" maxlength="2048" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.registrationUrl') }}</label>
            <input v-model.trim="form.registration_url" type="url" class="input" maxlength="2048" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.registrationDeadline') }}</label>
            <input v-model="form.registration_deadline" type="datetime-local" class="input" />
          </div>
          <div class="md:col-span-2">
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.coverUrl') }}</label>
            <input v-model.trim="form.cover_url" type="url" class="input" maxlength="2048" />
          </div>
          <div>
            <label class="mb-1 block text-sm text-gray-600 dark:text-gray-400">{{ t('events.fields.feeType') }}</label>
            <select v-model="form.fee_type" class="input">
              <option value="unknown">{{ t('events.fee.unknown') }}</option>
              <option value="free">{{ t('events.fee.free') }}</option>
              <option value="paid">{{ t('events.fee.paid') }}</option>
            </select>
          </div>
          <div v-if="form.fee_type === 'paid'" class="grid grid-cols-[1fr_1fr_88px] gap-2">
            <input v-model="form.price_min" type="number" min="0" step="0.01" class="input" :placeholder="t('events.fields.priceMin')" />
            <input v-model="form.price_max" type="number" min="0" step="0.01" class="input" :placeholder="t('events.fields.priceMax')" />
            <input v-model.trim="form.currency" class="input uppercase" maxlength="8" />
          </div>
        </div>
      </section>

      <section class="border-t border-gray-200 pt-6 dark:border-dark-700">
        <div class="mb-4 flex items-center justify-between">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('events.sections.occurrences') }}</h3>
          <button type="button" class="btn btn-secondary btn-sm" @click="addOccurrence">
            <Icon name="plus" size="sm" class="mr-1" />{{ t('events.actions.addOccurrence') }}
          </button>
        </div>
        <div class="space-y-6">
          <div
            v-for="(occurrence, index) in form.occurrences"
            :key="occurrence.key"
            class="border-b border-gray-200 pb-6 last:border-b-0 last:pb-0 dark:border-dark-700"
          >
            <div class="mb-3 flex items-center justify-between">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.sections.occurrenceNumber', { number: index + 1 }) }}</span>
              <button
                v-if="form.occurrences.length > 1"
                type="button"
                class="btn btn-ghost btn-sm text-red-600"
                :title="t('events.actions.removeOccurrence')"
                @click="removeOccurrence(index)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <div>
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.startsAt') }}</label>
                <input v-model="occurrence.starts_at" type="datetime-local" class="input" required />
              </div>
              <div>
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.endsAt') }}</label>
                <input v-model="occurrence.ends_at" type="datetime-local" class="input" />
              </div>
              <div>
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.timezone') }}</label>
                <input v-model.trim="occurrence.timezone" class="input" maxlength="64" required />
              </div>
              <label class="flex items-end gap-2 pb-2 text-sm text-gray-600 dark:text-gray-300">
                <input v-model="occurrence.all_day" type="checkbox" class="h-4 w-4 border-gray-300 text-primary-600" />
                {{ t('events.fields.allDay') }}
              </label>
              <div>
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.locationMode') }}</label>
                <select v-model="occurrence.location_mode" class="input">
                  <option value="offline">{{ t('events.location.offline') }}</option>
                  <option value="online">{{ t('events.location.online') }}</option>
                  <option value="hybrid">{{ t('events.location.hybrid') }}</option>
                </select>
              </div>
              <div v-if="occurrence.location_mode !== 'online'" class="xl:col-span-2">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.venueName') }}</label>
                <input v-model.trim="occurrence.venue_name" class="input" maxlength="300" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'" class="xl:col-span-2">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.address') }}</label>
                <input v-model.trim="occurrence.address" class="input" maxlength="1000" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.country') }}</label>
                <input v-model.trim="occurrence.country" class="input" maxlength="100" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.province') }}</label>
                <input v-model.trim="occurrence.province" class="input" maxlength="100" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.city') }}</label>
                <input v-model.trim="occurrence.city" class="input" maxlength="100" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.district') }}</label>
                <input v-model.trim="occurrence.district" class="input" maxlength="100" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.latitude') }}</label>
                <input v-model="occurrence.latitude" type="number" min="-90" max="90" step="0.000001" class="input" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.longitude') }}</label>
                <input v-model="occurrence.longitude" type="number" min="-180" max="180" step="0.000001" class="input" />
              </div>
              <div v-if="occurrence.location_mode !== 'online'" class="flex items-end xl:col-span-4">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="!mapSettings?.amap_key"
                  @click="toggleMapPicker(index)"
                >
                  <Icon name="globe" size="sm" class="mr-1" />{{ t('events.map.pickPoint') }}
                </button>
              </div>
              <div v-if="occurrence.location_mode !== 'online' && selectingOccurrenceIndex === index" class="xl:col-span-4">
                <p class="mb-2 text-xs text-gray-500">{{ t('events.map.selectionHint') }}</p>
                <div class="h-80 overflow-hidden border border-gray-200 dark:border-dark-700">
                  <EventMap
                    :api-key="mapSettings?.amap_key || ''"
                    :security-code="mapSettings?.security_code || ''"
                    :center="pickerCenter"
                    :zoom="mapSettings?.default_zoom || 14"
                    :markers="pickerMarkers"
                    selectable
                    @select="selectCoordinates"
                  />
                </div>
              </div>
              <div v-if="occurrence.location_mode !== 'offline'" class="md:col-span-2 xl:col-span-4">
                <label class="mb-1 block text-xs text-gray-500">{{ t('events.fields.onlineUrl') }}</label>
                <input v-model.trim="occurrence.online_url" type="url" class="input" maxlength="2048" />
              </div>
            </div>
          </div>
        </div>
      </section>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" form="event-editor-form" class="btn btn-primary" :disabled="saving || !canSubmit">
          <Icon v-if="saving" name="refresh" size="sm" class="mr-1 animate-spin" />
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import EventMap from '@/components/events/EventMap.vue'
import type { AdminGroup, EventCategory, EventMapMarker, EventMapSettings, EventOccurrence, EventStatus, EventVisibility, EventWriteRequest, TeamEvent } from '@/types'

const props = defineProps<{
  show: boolean
  event: TeamEvent | null
  categories: EventCategory[]
  groups: AdminGroup[]
  mapSettings: EventMapSettings | null
  saving: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'save', payload: EventWriteRequest): void
}>()

const { t } = useI18n()

interface OccurrenceDraft {
  key: number
  starts_at: string
  ends_at: string
  timezone: string
  all_day: boolean
  location_mode: 'offline' | 'online' | 'hybrid'
  online_url: string
  venue_name: string
  address: string
  country: string
  province: string
  city: string
  district: string
  latitude: string
  longitude: string
}

const form = reactive({
  category_id: '',
  title: '',
  summary: '',
  description_markdown: '',
  tags: '',
  organizer_name: '',
  organizer_url: '',
  registration_url: '',
  registration_deadline: '',
  cover_url: '',
  fee_type: 'unknown' as 'free' | 'paid' | 'unknown',
  price_min: '',
  price_max: '',
  currency: 'CNY',
  status: 'draft' as EventStatus,
  cancelled_reason: '',
  visibility: 'authenticated' as EventVisibility,
  subscription_group_ids: [] as number[],
  visible_from: '',
  visible_until: '',
  occurrences: [] as OccurrenceDraft[],
})

let nextOccurrenceKey = 1
const selectingOccurrenceIndex = ref(-1)
const audienceGroups = computed(() => [...props.groups].sort((left, right) => {
  if (left.status !== right.status) return left.status === 'active' ? -1 : 1
  if (left.platform !== right.platform) return left.platform.localeCompare(right.platform)
  return left.name.localeCompare(right.name)
}))
const canSubmit = computed(() => {
  if (form.visibility === 'targeted' && form.subscription_group_ids.length === 0) return false
  if (form.status === 'cancelled' && !form.cancelled_reason.trim()) return false
  if (form.visible_from && form.visible_until && new Date(form.visible_from) >= new Date(form.visible_until)) return false
  return true
})

const selectedOccurrence = computed(() => form.occurrences[selectingOccurrenceIndex.value])
const pickerCenter = computed<[number, number]>(() => {
  const occurrence = selectedOccurrence.value
  const latitude = occurrence?.latitude === '' ? Number.NaN : Number(occurrence?.latitude)
  const longitude = occurrence?.longitude === '' ? Number.NaN : Number(occurrence?.longitude)
  if (Number.isFinite(latitude) && Number.isFinite(longitude)) return [latitude, longitude]
  return [props.mapSettings?.default_latitude ?? 31.2304, props.mapSettings?.default_longitude ?? 121.4737]
})
const pickerMarkers = computed<EventMapMarker[]>(() => {
  const occurrence = selectedOccurrence.value
  const latitude = occurrence?.latitude === '' ? Number.NaN : Number(occurrence?.latitude)
  const longitude = occurrence?.longitude === '' ? Number.NaN : Number(occurrence?.longitude)
  if (!occurrence || !Number.isFinite(latitude) || !Number.isFinite(longitude)) return []
  return [{
    event_id: props.event?.id ?? 0,
    occurrence_id: 0,
    title: form.title || t('events.admin.create'),
    summary: form.summary,
    status: form.status,
    phase: 'upcoming',
    fee_type: form.fee_type,
    starts_at: occurrence.starts_at,
    ends_at: occurrence.ends_at || null,
    venue_name: occurrence.venue_name,
    address: occurrence.address,
    city: occurrence.city,
    district: occurrence.district,
    latitude,
    longitude,
  }]
})

function blankOccurrence(): OccurrenceDraft {
  const start = new Date(Date.now() + 24 * 60 * 60 * 1000)
  start.setMinutes(0, 0, 0)
  const end = new Date(start.getTime() + 2 * 60 * 60 * 1000)
  return {
    key: nextOccurrenceKey++,
    starts_at: toLocalInput(start.toISOString()),
    ends_at: toLocalInput(end.toISOString()),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai',
    all_day: false,
    location_mode: 'offline',
    online_url: '',
    venue_name: '',
    address: '',
    country: '中国',
    province: '',
    city: '',
    district: '',
    latitude: '',
    longitude: '',
  }
}

function occurrenceToDraft(value: EventOccurrence): OccurrenceDraft {
  return {
    key: nextOccurrenceKey++,
    starts_at: toLocalInput(value.starts_at),
    ends_at: toLocalInput(value.ends_at || ''),
    timezone: value.timezone || 'Asia/Shanghai',
    all_day: value.all_day,
    location_mode: value.location_mode,
    online_url: value.online_url || '',
    venue_name: value.venue_name || '',
    address: value.address || '',
    country: value.country || '中国',
    province: value.province || '',
    city: value.city || '',
    district: value.district || '',
    latitude: value.latitude == null ? '' : String(value.latitude),
    longitude: value.longitude == null ? '' : String(value.longitude),
  }
}

function reset() {
  selectingOccurrenceIndex.value = -1
  const value = props.event
  form.category_id = value?.category_id ? String(value.category_id) : ''
  form.title = value?.title || ''
  form.summary = value?.summary || ''
  form.description_markdown = value?.description_markdown || ''
  form.tags = value?.tags.join(', ') || ''
  form.organizer_name = value?.organizer_name || ''
  form.organizer_url = value?.organizer_url || ''
  form.registration_url = value?.registration_url || ''
  form.registration_deadline = toLocalInput(value?.registration_deadline || '')
  form.cover_url = value?.cover_url || ''
  form.fee_type = value?.fee_type || 'unknown'
  form.price_min = value?.price_min == null ? '' : String(value.price_min)
  form.price_max = value?.price_max == null ? '' : String(value.price_max)
  form.currency = value?.currency || 'CNY'
  form.status = value?.status || 'draft'
  form.cancelled_reason = value?.cancelled_reason || ''
  form.visibility = value?.visibility || 'authenticated'
  form.subscription_group_ids = [...(value?.audience.subscription_group_ids || [])]
  form.visible_from = toLocalInput(value?.visible_from || '')
  form.visible_until = toLocalInput(value?.visible_until || '')
  form.occurrences = value?.occurrences.length ? value.occurrences.map(occurrenceToDraft) : [blankOccurrence()]
}

function addOccurrence() {
  if (form.occurrences.length < 50) form.occurrences.push(blankOccurrence())
}

function removeOccurrence(index: number) {
  form.occurrences.splice(index, 1)
  if (selectingOccurrenceIndex.value === index) selectingOccurrenceIndex.value = -1
  else if (selectingOccurrenceIndex.value > index) selectingOccurrenceIndex.value -= 1
}

function toggleMapPicker(index: number) {
  selectingOccurrenceIndex.value = selectingOccurrenceIndex.value === index ? -1 : index
}

function selectCoordinates(value: { latitude: number; longitude: number }) {
  const occurrence = selectedOccurrence.value
  if (!occurrence) return
  occurrence.latitude = value.latitude.toFixed(6)
  occurrence.longitude = value.longitude.toFixed(6)
}

function submit() {
  const occurrences: EventOccurrence[] = form.occurrences.map((value) => ({
    starts_at: new Date(value.starts_at).toISOString(),
    ends_at: value.ends_at ? new Date(value.ends_at).toISOString() : null,
    timezone: value.timezone,
    all_day: value.all_day,
    location_mode: value.location_mode,
    online_url: value.online_url,
    venue_name: value.venue_name,
    address: value.address,
    country: value.country,
    province: value.province,
    city: value.city,
    district: value.district,
    latitude: value.latitude === '' ? null : Number(value.latitude),
    longitude: value.longitude === '' ? null : Number(value.longitude),
    coordinate_source: 'wgs84',
  }))
  emit('save', {
    category_id: form.category_id ? Number(form.category_id) : null,
    title: form.title,
    summary: form.summary,
    description_markdown: form.description_markdown,
    tags: form.tags.split(/[,，]/).map((value) => value.trim()).filter(Boolean).slice(0, 20),
    organizer_name: form.organizer_name,
    organizer_url: form.organizer_url,
    fee_type: form.fee_type,
    price_min: form.price_min === '' ? null : Number(form.price_min),
    price_max: form.price_max === '' ? null : Number(form.price_max),
    currency: form.currency,
    registration_url: form.registration_url,
    registration_deadline: form.registration_deadline ? new Date(form.registration_deadline).toISOString() : null,
    cover_url: form.cover_url,
    status: form.status,
    visibility: form.visibility,
    audience: form.visibility === 'targeted' ? { subscription_group_ids: [...form.subscription_group_ids] } : {},
    visible_from: form.visible_from ? new Date(form.visible_from).toISOString() : null,
    visible_until: form.visible_until ? new Date(form.visible_until).toISOString() : null,
    cancelled_reason: form.status === 'cancelled' ? form.cancelled_reason : '',
    occurrences,
  })
}

function toLocalInput(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function close() {
  if (!props.saving) emit('close')
}

watch(() => [props.show, props.event] as const, ([show]) => {
  if (show) reset()
}, { immediate: true })
</script>
