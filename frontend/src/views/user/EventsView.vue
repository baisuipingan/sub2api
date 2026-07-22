<template>
  <AppLayout>
    <div class="flex min-h-[calc(100vh-8rem)] flex-col">
      <div class="mb-4 flex flex-wrap items-center gap-3">
        <div class="relative min-w-52 flex-1 sm:max-w-72">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input v-model="filters.search" class="input pl-9" :placeholder="t('events.user.search')" @input="scheduleReload" />
        </div>
        <select v-model="filters.category" class="input w-40" @change="reloadAll">
          <option value="">{{ t('events.admin.allCategories') }}</option>
          <option v-for="category in categories" :key="category.id" :value="category.code">{{ category.name }}</option>
        </select>
        <select v-model="filters.fee_type" class="input w-36" @change="reloadAll">
          <option value="">{{ t('events.user.allFees') }}</option>
          <option value="free">{{ t('events.fee.free') }}</option>
          <option value="paid">{{ t('events.fee.paid') }}</option>
        </select>
        <input v-model.trim="filters.city" class="input w-36" :placeholder="t('events.fields.city')" @input="scheduleReload" />
        <div class="ml-auto inline-flex border border-gray-200 p-1 lg:hidden dark:border-dark-700">
          <button class="px-3 py-1.5 text-sm" :class="mobileView === 'list' ? activeModeClass : inactiveModeClass" @click="setMobileView('list')"><Icon name="list" size="sm" class="mr-1 inline" />{{ t('events.user.list') }}</button>
          <button class="px-3 py-1.5 text-sm" :class="mobileView === 'map' ? activeModeClass : inactiveModeClass" @click="setMobileView('map')"><Icon name="globe" size="sm" class="mr-1 inline" />{{ t('events.user.map') }}</button>
        </div>
      </div>

      <div class="min-h-0 flex-1 overflow-hidden border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900 lg:grid lg:grid-cols-[380px_minmax(0,1fr)]">
        <section :class="mobileView === 'list' ? 'flex' : 'hidden lg:flex'" class="min-h-[560px] flex-col border-r border-gray-200 dark:border-dark-700">
          <div class="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-dark-700">
            <span class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('events.user.resultCount', { count: pagination.total }) }}</span>
            <button class="btn btn-ghost btn-sm" :disabled="loadingList" :title="t('common.refresh')" @click="reloadAll"><Icon name="refresh" size="sm" :class="loadingList ? 'animate-spin' : ''" /></button>
          </div>
          <div class="min-h-0 flex-1 overflow-y-auto">
            <article
              v-for="event in events"
              :key="event.id"
              class="relative border-b border-gray-100 transition-colors dark:border-dark-800"
              :class="selectedMapEventId === event.id ? 'bg-primary-50 dark:bg-primary-950/20' : 'hover:bg-gray-50 dark:hover:bg-dark-800'"
            >
              <button
                type="button"
                class="block w-full px-4 py-4 pr-14 text-left"
                :title="eventHasMapLocation(event) ? t('events.map.locate') : t('events.user.viewDetails')"
                @click="focusEventOnMap(event)"
              >
                <div class="flex items-start gap-3">
                  <span class="mt-1 h-3 w-3 flex-none" :style="{ backgroundColor: event.category?.color || '#6B7280' }" />
                  <div class="min-w-0 flex-1">
                    <div class="flex items-start justify-between gap-2">
                      <h2 class="line-clamp-2 text-sm font-semibold text-gray-900 dark:text-white">{{ event.title }}</h2>
                      <span class="badge flex-none text-[10px]" :class="phaseBadge(event.phase)">{{ t(`events.phase.${event.phase}`) }}</span>
                    </div>
                    <p v-if="event.summary" class="mt-1 line-clamp-2 text-xs leading-5 text-gray-500">{{ event.summary }}</p>
                    <div v-if="event.occurrences[0]" class="mt-3 space-y-1 text-xs text-gray-600 dark:text-gray-300">
                      <div class="flex items-center gap-2"><Icon name="calendar" size="xs" />{{ formatDateTime(event.occurrences[0].starts_at) }}</div>
                      <div class="flex min-w-0 items-center gap-2">
                        <Icon name="mapPin" size="xs" />
                        <span class="truncate">{{ event.occurrences[0].venue_name || event.occurrences[0].address || t(`events.location.${event.occurrences[0].location_mode}`) }}</span>
                      </div>
                    </div>
                    <div class="mt-3 flex flex-wrap items-center gap-2">
                      <span v-if="event.category" class="text-xs text-gray-500">{{ event.category.name }}</span>
                      <span class="text-xs font-medium" :class="event.fee_type === 'free' ? 'text-emerald-600' : 'text-gray-500'">{{ feeLabel(event) }}</span>
                      <span v-if="!eventHasMapLocation(event)" class="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400">
                        <Icon name="mapPin" size="xs" />{{ t('events.user.noMapLocation') }}
                      </span>
                    </div>
                  </div>
                </div>
              </button>
              <button
                type="button"
                class="absolute bottom-3 right-3 flex h-8 w-8 items-center justify-center text-gray-400 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700"
                :title="t('events.user.viewDetails')"
                @click="openEvent(event.id)"
              >
                <Icon name="eye" size="sm" />
              </button>
            </article>
            <div v-if="!loadingList && events.length === 0" class="flex min-h-64 items-center justify-center px-6 text-center text-sm text-gray-500">{{ t('events.user.empty') }}</div>
          </div>
          <div v-if="pagination.pages > 1" class="flex items-center justify-between border-t border-gray-200 px-4 py-3 dark:border-dark-700">
            <button class="btn btn-secondary btn-sm" :disabled="pagination.page <= 1" @click="goPage(pagination.page - 1)"><Icon name="chevronLeft" size="sm" /></button>
            <span class="text-xs text-gray-500">{{ pagination.page }} / {{ pagination.pages }}</span>
            <button class="btn btn-secondary btn-sm" :disabled="pagination.page >= pagination.pages" @click="goPage(pagination.page + 1)"><Icon name="chevronRight" size="sm" /></button>
          </div>
        </section>

        <section :class="mobileView === 'map' ? 'block' : 'hidden lg:block'" class="min-h-[560px]">
          <EventMap
            ref="eventMapRef"
            class="h-[560px] lg:h-full"
            :api-key="mapConfig.apiKey"
            :security-code="mapConfig.securityCode"
            :center="mapConfig.center"
            :zoom="mapConfig.zoom"
            :markers="markers"
            :selected-occurrence-id="selectedOccurrenceId"
            :fit-request="fitRequest"
            @bounds-change="handleBoundsChange"
            @marker-select="selectMapMarker"
            @marker-details="(marker) => openEvent(marker.event_id)"
          />
        </section>
      </div>
    </div>

    <BaseDialog :show="showDetail" :title="selectedEvent?.title || t('events.user.detail')" width="wide" @close="closeDetail">
      <div v-if="detailLoading" class="flex min-h-48 items-center justify-center"><Icon name="refresh" size="lg" class="animate-spin text-primary-600" /></div>
      <div v-else-if="selectedEvent" class="space-y-6">
        <img v-if="selectedEvent.cover_url" :src="selectedEvent.cover_url" :alt="selectedEvent.title" class="aspect-[16/7] w-full border border-gray-200 object-cover dark:border-dark-700" loading="lazy" />
        <div v-if="selectedEvent.status === 'cancelled'" class="border-l-4 border-red-500 bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
          <div class="font-semibold">{{ t('events.status.cancelled') }}</div>
          <div class="mt-1">{{ selectedEvent.cancelled_reason }}</div>
        </div>
        <div class="flex flex-wrap gap-2">
          <span v-if="selectedEvent.category" class="badge badge-primary">{{ selectedEvent.category.name }}</span>
          <span class="badge" :class="phaseBadge(selectedEvent.phase)">{{ t(`events.phase.${selectedEvent.phase}`) }}</span>
          <span class="badge badge-gray">{{ feeLabel(selectedEvent) }}</span>
        </div>
        <p v-if="selectedEvent.summary" class="text-base leading-7 text-gray-700 dark:text-gray-300">{{ selectedEvent.summary }}</p>
        <div class="divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
          <div v-for="(occurrence, index) in selectedEvent.occurrences" :key="occurrence.id || index" class="grid gap-2 py-4 text-sm sm:grid-cols-[150px_1fr]">
            <div class="font-medium text-gray-900 dark:text-white">{{ formatDateTime(occurrence.starts_at) }}</div>
            <div class="text-gray-600 dark:text-gray-300">
              <div>{{ occurrence.venue_name || t(`events.location.${occurrence.location_mode}`) }}</div>
              <div class="mt-1 text-xs text-gray-500">{{ occurrence.address }}</div>
            </div>
          </div>
        </div>
        <div v-if="selectedEvent.description_markdown" class="event-markdown prose prose-sm max-w-none dark:prose-invert" v-html="renderMarkdown(selectedEvent.description_markdown)" />
        <div v-if="selectedEvent.organizer_name" class="text-sm text-gray-600 dark:text-gray-300">{{ t('events.fields.organizerName') }}：{{ selectedEvent.organizer_name }}</div>
      </div>
      <template #footer>
        <div class="flex w-full justify-between gap-3">
          <button class="btn btn-secondary" @click="closeDetail">{{ t('common.close') }}</button>
          <a v-if="selectedEvent?.registration_url && selectedEvent.status !== 'cancelled'" :href="selectedEvent.registration_url" target="_blank" rel="noopener noreferrer" class="btn btn-primary"><Icon name="externalLink" size="sm" class="mr-1" />{{ t('events.user.register') }}</a>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import eventsAPI from '@/api/events'
import { useAppStore } from '@/stores/app'
import { formatCurrency, formatDateTime } from '@/utils/format'
import type { EventCategory, EventMapMarker, UserEvent } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import EventMap from '@/components/events/EventMap.vue'

const { t } = useI18n()
const appStore = useAppStore()
const events = ref<UserEvent[]>([])
const markers = ref<EventMapMarker[]>([])
const eventMapRef = ref<InstanceType<typeof EventMap> | null>(null)
const categories = ref<EventCategory[]>([])
const loadingList = ref(false)
const filters = reactive({ search: '', category: '', city: '', fee_type: '' })
const pagination = reactive({ page: 1, page_size: 30, total: 0, pages: 1 })
const mobileView = ref<'list' | 'map'>('list')
const activeModeClass = 'bg-primary-600 text-white'
const inactiveModeClass = 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'
const bbox = ref('')
const fitRequest = ref(1)
const mapResultsTruncated = ref(false)
const selectedOccurrenceId = ref<number | null>(null)
const selectedMapEventId = ref<number | null>(null)
const showDetail = ref(false)
const detailLoading = ref(false)
const selectedEvent = ref<UserEvent | null>(null)
let listController: AbortController | null = null
let mapController: AbortController | null = null
let searchTimer: number | null = null
let mapTimer: number | null = null

const mapConfig = computed(() => ({
  apiKey: appStore.cachedPublicSettings?.event_map_amap_key || '',
  securityCode: appStore.cachedPublicSettings?.event_map_amap_security_code || '',
  center: [
    appStore.cachedPublicSettings?.event_map_default_latitude ?? 31.2304,
    appStore.cachedPublicSettings?.event_map_default_longitude ?? 121.4737,
  ] as [number, number],
  zoom: appStore.cachedPublicSettings?.event_map_default_zoom ?? 11,
}))

function requestFilters() {
  return {
    search: filters.search || undefined,
    category: filters.category || undefined,
    city: filters.city || undefined,
    fee_type: filters.fee_type || undefined,
  }
}

async function loadList() {
  listController?.abort()
  const request = new AbortController()
  listController = request
  loadingList.value = true
  try {
    const result = await eventsAPI.list(pagination.page, pagination.page_size, requestFilters(), request.signal)
    if (listController !== request) return
    events.value = result.items
    Object.assign(pagination, { page: result.page, page_size: result.page_size, total: result.total, pages: result.pages })
  } catch (error: any) {
    if (error?.code !== 'ERR_CANCELED') appStore.showError(error?.message || t('events.user.loadFailed'))
  } finally {
    if (listController === request) {
      listController = null
      loadingList.value = false
    }
  }
}

async function loadMarkers(useCurrentBounds = false) {
  mapController?.abort()
  const request = new AbortController()
  mapController = request
  const requestedBBox = useCurrentBounds && mapResultsTruncated.value ? bbox.value : ''
  try {
    const result = await eventsAPI.map({ ...requestFilters(), bbox: requestedBBox || undefined }, request.signal)
    if (mapController === request) {
      markers.value = result.markers
      if (!requestedBBox) mapResultsTruncated.value = result.truncated
    }
  } catch (error: any) {
    if (error?.code !== 'ERR_CANCELED') appStore.showError(error?.message || t('events.map.loadFailed'))
  } finally {
    if (mapController === request) mapController = null
  }
}

function reloadAll() {
  pagination.page = 1
  bbox.value = ''
  mapResultsTruncated.value = false
  selectedOccurrenceId.value = null
  selectedMapEventId.value = null
  fitRequest.value += 1
  void Promise.all([loadList(), loadMarkers(false)])
}
function scheduleReload() {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reloadAll, 300)
}
function handleBoundsChange(value: string) {
  bbox.value = value
  if (!mapResultsTruncated.value) return
  if (mapTimer) window.clearTimeout(mapTimer)
  mapTimer = window.setTimeout(() => { void loadMarkers(true) }, 250)
}
function goPage(page: number) { pagination.page = page; void loadList() }

function eventHasMapLocation(event: UserEvent): boolean {
  return event.occurrences.some((occurrence) =>
    Number.isFinite(occurrence.latitude) && Number.isFinite(occurrence.longitude),
  )
}

function selectMapMarker(marker: EventMapMarker) {
  selectedOccurrenceId.value = marker.occurrence_id
  selectedMapEventId.value = marker.event_id
}

function setMobileView(view: 'list' | 'map') {
  mobileView.value = view
  if (view === 'map') {
    void nextTick(() => eventMapRef.value?.refreshViewport())
  }
}

function focusEventOnMap(event: UserEvent) {
  const marker = markers.value.find((item) => item.event_id === event.id)
  if (!marker) {
    void openEvent(event.id)
    return
  }
  selectMapMarker(marker)
  if (window.matchMedia('(max-width: 1023px)').matches) setMobileView('map')
}

async function openEvent(id: number) {
  showDetail.value = true
  detailLoading.value = true
  selectedEvent.value = null
  try {
    selectedEvent.value = await eventsAPI.getById(id)
  } catch (error: any) {
    appStore.showError(error?.message || t('events.user.detailFailed'))
    showDetail.value = false
  } finally {
    detailLoading.value = false
  }
}
function closeDetail() { showDetail.value = false; selectedEvent.value = null }
function phaseBadge(phase: UserEvent['phase']) {
  if (phase === 'ongoing') return 'badge-success'
  if (phase === 'cancelled') return 'badge-danger'
  if (phase === 'upcoming') return 'badge-primary'
  return 'badge-gray'
}
function feeLabel(event: UserEvent) {
  if (event.fee_type === 'free') return t('events.fee.free')
  if (event.fee_type === 'paid' && event.price_min != null) return formatCurrency(event.price_min, event.currency)
  return t(`events.fee.${event.fee_type}`)
}
function renderMarkdown(value: string) {
  return DOMPurify.sanitize(marked.parse(value, { async: false }) as string)
}

onMounted(async () => {
  await appStore.fetchPublicSettings()
  try { categories.value = await eventsAPI.categories() } catch { categories.value = [] }
  await Promise.all([loadList(), loadMarkers()])
})
onUnmounted(() => {
  listController?.abort(); mapController?.abort()
  if (searchTimer) window.clearTimeout(searchTimer)
  if (mapTimer) window.clearTimeout(mapTimer)
})
</script>

<style scoped>
.event-markdown :deep(a) {
  color: #2563eb;
  text-decoration: underline;
}
</style>
