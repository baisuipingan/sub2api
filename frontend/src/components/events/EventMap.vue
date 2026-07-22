<template>
  <div class="relative h-full min-h-80 w-full overflow-hidden bg-gray-100 dark:bg-dark-900">
    <div ref="container" class="absolute inset-0" />

    <div
      v-if="apiKey && !loading && !loadError && markers.length > 0"
      class="absolute right-3 top-3 z-10 flex h-9 items-center border border-gray-200 bg-white/95 shadow-sm backdrop-blur dark:border-dark-700 dark:bg-dark-900/95"
    >
      <span class="border-r border-gray-200 px-3 text-xs font-medium text-gray-700 dark:border-dark-700 dark:text-gray-200">
        {{ t('events.map.markerCount', { count: markers.length }) }}
      </span>
      <button
        type="button"
        class="flex h-9 w-9 items-center justify-center text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:text-gray-300 dark:hover:bg-dark-800"
        :title="t('events.map.fitMarkers')"
        @click="fitMarkers"
      >
        <Icon name="mapPin" size="sm" />
      </button>
    </div>

    <div v-if="!apiKey" class="absolute inset-0 flex items-center justify-center bg-gray-100/95 px-6 text-center dark:bg-dark-900/95">
      <div>
        <Icon name="globe" size="xl" class="mx-auto mb-3 text-gray-400" />
        <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('events.map.notConfigured') }}</p>
      </div>
    </div>
    <div v-else-if="loading" class="absolute inset-0 flex items-center justify-center bg-white/70 dark:bg-dark-900/70">
      <Icon name="refresh" size="lg" class="animate-spin text-primary-600" />
    </div>
    <div v-else-if="loadError" class="absolute inset-0 flex items-center justify-center bg-white/90 px-6 text-center dark:bg-dark-900/90">
      <p class="text-sm text-red-600">{{ t('events.map.loadFailed') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { EventMapMarker } from '@/types'
import { gcj02ToWgs84, wgs84ToGcj02 } from '@/utils/eventCoordinates'

declare global {
  interface Window {
    AMap?: any
    _AMapSecurityConfig?: { securityJsCode?: string }
  }
}

const props = withDefaults(defineProps<{
  apiKey: string
  securityCode?: string
  center: [number, number]
  zoom: number
  markers?: EventMapMarker[]
  selectable?: boolean
  selectedOccurrenceId?: number | null
  fitRequest?: number
}>(), {
  securityCode: '',
  markers: () => [],
  selectable: false,
  selectedOccurrenceId: null,
  fitRequest: 0,
})

const emit = defineEmits<{
  (event: 'bounds-change', bbox: string): void
  (event: 'marker-select', marker: EventMapMarker): void
  (event: 'marker-details', marker: EventMapMarker): void
  (event: 'select', coordinates: { latitude: number; longitude: number }): void
}>()

const { t } = useI18n()
const container = ref<HTMLElement | null>(null)
const loading = ref(false)
const loadError = ref(false)
let map: any = null
let cluster: any = null
let infoWindow: any = null
let clusterPoints: Array<{ lnglat: [number, number]; item: EventMapMarker }> = []
let fitMarkerInstances: any[] = []
let fitMarkerByOccurrence = new Map<number, any>()
const clusterMarkerItems = new WeakMap<object, EventMapMarker>()
const boundClusterMarkers = new WeakSet<object>()
let resizeObserver: ResizeObserver | null = null
let lastAppliedFitRequest = -1
let loaderPromise: Promise<any> | null = null

function loadAMap(): Promise<any> {
  if (window.AMap) return Promise.resolve(window.AMap)
  if (loaderPromise) return loaderPromise
  window._AMapSecurityConfig = { securityJsCode: props.securityCode || undefined }
  loaderPromise = new Promise((resolve, reject) => {
    const callback = `__sub2apiAMapReady_${Date.now()}`
    ;(window as any)[callback] = () => {
      delete (window as any)[callback]
      resolve(window.AMap)
    }
    const script = document.createElement('script')
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(props.apiKey)}&plugin=AMap.MarkerCluster&callback=${callback}`
    script.async = true
    script.onerror = () => {
      delete (window as any)[callback]
      loaderPromise = null
      reject(new Error('AMap failed to load'))
    }
    document.head.appendChild(script)
  })
  return loaderPromise
}

async function initialize() {
  if (!props.apiKey || !container.value || map) return
  loading.value = true
  loadError.value = false
  try {
    const AMap = await loadAMap()
    const [lat, lng] = wgs84ToGcj02(props.center[0], props.center[1])
    map = new AMap.Map(container.value, {
      zoom: props.zoom,
      center: [lng, lat],
      viewMode: '2D',
      resizeEnable: true,
    })
    map.on('moveend', emitBounds)
    map.on('zoomend', emitBounds)
    if (props.selectable) {
      map.on('click', (event: any) => {
        const [latitude, longitude] = gcj02ToWgs84(event.lnglat.getLat(), event.lnglat.getLng())
        emit('select', { latitude, longitude })
      })
    }
    renderMarkers()
    resizeObserver = new ResizeObserver(() => map?.resize())
    resizeObserver.observe(container.value)
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function renderMarkers() {
  if (!map || !window.AMap) return
  closeInfoWindow()
  if (cluster) {
    cluster.setMap(null)
    cluster = null
  }
  fitMarkerInstances.forEach((instance) => instance.setMap(null))
  fitMarkerInstances = []
  fitMarkerByOccurrence = new Map<number, any>()
  clusterPoints = props.markers.map((item) => {
    const [lat, lng] = wgs84ToGcj02(item.latitude, item.longitude)
    return { lnglat: [lng, lat] as [number, number], item }
  })
  fitMarkerInstances = clusterPoints.map((point) => {
    const instance = new window.AMap.Marker({ position: point.lnglat })
    fitMarkerByOccurrence.set(point.item.occurrence_id, instance)
    return instance
  })
  if (clusterPoints.length > 0) {
    cluster = new window.AMap.MarkerCluster(map, clusterPoints, {
      gridSize: 70,
      maxZoom: 16,
      renderClusterMarker: renderClusterMarker,
      renderMarker: renderSingleMarker,
    })
  }
  fitMarkersIfRequested()
}

function createMarkerContent(item: EventMapMarker, selected: boolean): HTMLButtonElement {
  const button = document.createElement('button')
  button.type = 'button'
  button.className = `event-map-pin${selected ? ' event-map-pin--selected' : ''}`
  button.title = item.title
  button.setAttribute('aria-label', item.title)
  button.style.setProperty('--event-pin-color', markerColor(item))

  const shape = document.createElement('span')
  shape.className = 'event-map-pin__shape'
  const dot = document.createElement('span')
  dot.className = 'event-map-pin__dot'
  shape.appendChild(dot)
  button.appendChild(shape)
  return button
}

function renderClusterMarker(context: any) {
  const content = document.createElement('button')
  content.type = 'button'
  content.className = 'event-map-cluster'
  content.setAttribute('aria-label', t('events.map.clusterLabel', { count: context.count }))
  content.textContent = String(context.count)
  context.marker.setContent(content)
  if (window.AMap?.Pixel) context.marker.setOffset(new window.AMap.Pixel(-21, -21))
}

function renderSingleMarker(context: any) {
  const point = context.data?.[0] as { item?: EventMapMarker } | undefined
  const item = point?.item
  if (!item) return
  const instance = context.marker
  instance.setContent(createMarkerContent(item, item.occurrence_id === props.selectedOccurrenceId))
  instance.setzIndex?.(item.occurrence_id === props.selectedOccurrenceId ? 160 : 100)
  if (window.AMap?.Pixel) instance.setOffset(new window.AMap.Pixel(-18, -44))

  clusterMarkerItems.set(instance, item)
  if (!boundClusterMarkers.has(instance)) {
    boundClusterMarkers.add(instance)
    instance.on('click', () => {
      const selected = clusterMarkerItems.get(instance)
      if (!selected) return
      showInfoWindow(selected, instance.getPosition())
      emit('marker-select', selected)
    })
  }
}

function markerColor(item: EventMapMarker): string {
  const value = item.category?.color?.trim()
  return value && /^#[0-9a-f]{6}$/i.test(value) ? value : '#2563EB'
}

function focusSelectedMarker(openInfo = true) {
  if (!map || props.selectedOccurrenceId == null) return
  const point = clusterPoints.find(({ item }) => item.occurrence_id === props.selectedOccurrenceId)
  const position = fitMarkerByOccurrence.get(props.selectedOccurrenceId)?.getPosition()
  if (!point || !position) return
  const currentZoom = Number(map.getZoom?.()) || props.zoom
  if (currentZoom < 14 && map.setZoomAndCenter) map.setZoomAndCenter(14, position)
  else map.panTo?.(position)
  if (openInfo) showInfoWindow(point.item, position)
}

function showInfoWindow(item: EventMapMarker, position: any) {
  if (!map || !window.AMap?.InfoWindow) return
  closeInfoWindow()
  const content = createInfoContent(item)
  infoWindow = new window.AMap.InfoWindow({
    anchor: 'bottom-center',
    autoMove: false,
    content,
    isCustom: true,
    offset: window.AMap.Pixel ? new window.AMap.Pixel(0, -38) : undefined,
  })
  infoWindow.open(map, position)
}

function createInfoContent(item: EventMapMarker): HTMLDivElement {
  const root = document.createElement('div')
  root.className = 'event-map-popup'

  const closeButton = document.createElement('button')
  closeButton.type = 'button'
  closeButton.className = 'event-map-popup__close'
  closeButton.setAttribute('aria-label', t('common.close'))
  closeButton.textContent = '×'
  closeButton.addEventListener('click', closeInfoWindow)

  const category = document.createElement('div')
  category.className = 'event-map-popup__category'
  const categoryDot = document.createElement('span')
  categoryDot.style.backgroundColor = markerColor(item)
  category.appendChild(categoryDot)
  category.append(document.createTextNode(item.category?.name || t('events.common.uncategorized')))

  const title = document.createElement('h3')
  title.className = 'event-map-popup__title'
  title.textContent = item.title

  const date = document.createElement('div')
  date.className = 'event-map-popup__meta'
  date.textContent = formatMarkerDate(item.starts_at)

  const location = document.createElement('div')
  location.className = 'event-map-popup__meta'
  location.textContent = item.venue_name || item.address || [item.city, item.district].filter(Boolean).join(' ') || t('events.location.offline')

  const details = document.createElement('button')
  details.type = 'button'
  details.className = 'event-map-popup__details'
  details.textContent = t('events.map.viewDetails')
  details.addEventListener('click', () => emit('marker-details', item))

  root.append(closeButton, category, title, date, location, details)
  return root
}

function formatMarkerDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const locale = document.documentElement.lang.trim() || undefined
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

function closeInfoWindow() {
  infoWindow?.close?.()
  infoWindow = null
}

function fitMarkersIfRequested() {
  if (props.fitRequest === lastAppliedFitRequest || fitMarkerInstances.length === 0) return
  lastAppliedFitRequest = props.fitRequest
  fitMarkers()
}

function fitMarkers() {
  if (!map || fitMarkerInstances.length === 0) return
  closeInfoWindow()
  if (fitMarkerInstances.length === 1) {
    map.setZoomAndCenter?.(14, fitMarkerInstances[0].getPosition())
    return
  }
  map.setFitView?.(fitMarkerInstances, false, [56, 56, 56, 56], 14)
}

function refreshViewport() {
  window.requestAnimationFrame(() => {
    map?.resize?.()
    if (props.selectedOccurrenceId != null) focusSelectedMarker(false)
    else fitMarkers()
  })
}

function emitBounds() {
  if (!map) return
  const bounds = map.getBounds()
  const southWest = bounds.getSouthWest()
  const northEast = bounds.getNorthEast()
  const [minLat, minLng] = gcj02ToWgs84(southWest.getLat(), southWest.getLng())
  const [maxLat, maxLng] = gcj02ToWgs84(northEast.getLat(), northEast.getLng())
  emit('bounds-change', [minLng, minLat, maxLng, maxLat].map((value) => value.toFixed(6)).join(','))
}

watch(() => props.markers, renderMarkers, { deep: true })
watch(() => props.selectedOccurrenceId, () => {
  cluster?.setData?.(clusterPoints)
  if (props.selectedOccurrenceId == null) closeInfoWindow()
  else focusSelectedMarker()
})
watch(() => [props.apiKey, props.securityCode], () => {
  if (!map) void initialize()
})

defineExpose({ refreshViewport })

onMounted(() => { void initialize() })
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  closeInfoWindow()
  if (cluster) cluster.setMap(null)
  fitMarkerInstances.forEach((instance) => instance.setMap(null))
  map?.destroy()
  map = null
})
</script>

<style>
.event-map-pin {
  position: relative;
  width: 36px;
  height: 44px;
  border: 0;
  padding: 0;
  background: transparent;
  cursor: pointer;
  filter: drop-shadow(0 3px 5px rgba(15, 23, 42, 0.28));
}

.event-map-pin__shape {
  position: absolute;
  left: 5px;
  top: 3px;
  width: 27px;
  height: 27px;
  border: 3px solid white;
  border-radius: 50% 50% 50% 0;
  background: var(--event-pin-color, #2563eb);
  transform: rotate(-45deg);
  transition: transform 150ms ease, box-shadow 150ms ease;
}

.event-map-pin__dot {
  position: absolute;
  left: 7px;
  top: 7px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: white;
}

.event-map-pin:hover .event-map-pin__shape,
.event-map-pin--selected .event-map-pin__shape {
  box-shadow: 0 0 0 3px white, 0 0 0 6px var(--event-pin-color, #2563eb);
  transform: rotate(-45deg) scale(1.14);
}

.event-map-cluster {
  display: flex;
  width: 42px;
  height: 42px;
  align-items: center;
  justify-content: center;
  border: 3px solid white;
  border-radius: 50%;
  background: #111827;
  color: white;
  font-size: 13px;
  font-weight: 700;
  box-shadow: 0 3px 10px rgba(15, 23, 42, 0.3);
}

.event-map-popup {
  position: relative;
  width: min(280px, calc(100vw - 40px));
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  background: white;
  padding: 14px 16px 12px;
  color: #111827;
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.2);
}

.event-map-popup::after {
  position: absolute;
  left: 50%;
  bottom: -7px;
  width: 12px;
  height: 12px;
  border-right: 1px solid #e5e7eb;
  border-bottom: 1px solid #e5e7eb;
  background: white;
  content: '';
  transform: translateX(-50%) rotate(45deg);
}

.event-map-popup__close {
  position: absolute;
  right: 7px;
  top: 5px;
  width: 28px;
  height: 28px;
  border: 0;
  background: transparent;
  color: #6b7280;
  font-size: 22px;
  line-height: 26px;
  cursor: pointer;
}

.event-map-popup__category {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-right: 24px;
  color: #6b7280;
  font-size: 11px;
}

.event-map-popup__category > span {
  width: 8px;
  height: 8px;
  flex: none;
}

.event-map-popup__title {
  margin: 7px 24px 8px 0;
  overflow: hidden;
  color: #111827;
  font-size: 14px;
  font-weight: 650;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.event-map-popup__meta {
  margin-top: 4px;
  overflow: hidden;
  color: #4b5563;
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.event-map-popup__details {
  position: relative;
  z-index: 1;
  margin-top: 10px;
  border: 0;
  padding: 0;
  background: transparent;
  color: #2563eb;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

html.dark .event-map-popup {
  border-color: #374151;
  background: #111827;
  color: #f9fafb;
}

html.dark .event-map-popup::after {
  border-color: #374151;
  background: #111827;
}

html.dark .event-map-popup__title {
  color: #f9fafb;
}

html.dark .event-map-popup__meta,
html.dark .event-map-popup__category {
  color: #d1d5db;
}
</style>
