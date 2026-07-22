import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import EventMap from '../EventMap.vue'
import type { EventMapMarker } from '@/types'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params?.count != null ? `${key}:${params.count}` : key,
  }),
}))

class FakeMap {
  static instances: FakeMap[] = []
  handlers = new Map<string, (...args: any[]) => void>()
  setFitView = vi.fn()
  setZoomAndCenter = vi.fn()
  panTo = vi.fn()
  resize = vi.fn()
  destroy = vi.fn()

  constructor(_container: HTMLElement, _options: Record<string, unknown>) {
    FakeMap.instances.push(this)
  }

  on(name: string, handler: (...args: any[]) => void) { this.handlers.set(name, handler) }
  getZoom() { return 11 }
  getBounds() {
    return {
      getSouthWest: () => ({ getLat: () => 30, getLng: () => 120 }),
      getNorthEast: () => ({ getLat: () => 32, getLng: () => 122 }),
    }
  }
}

class FakeMarker {
  static instances: FakeMarker[] = []
  handlers = new Map<string, (...args: any[]) => void>()
  content: HTMLElement
  setMap = vi.fn()
  setzIndex = vi.fn()
  setOffset = vi.fn()

  constructor(public options: Record<string, any>) {
    this.content = options.content
    FakeMarker.instances.push(this)
  }

  on(name: string, handler: (...args: any[]) => void) { this.handlers.set(name, handler) }
  setContent(content: HTMLElement) { this.content = content }
  getPosition() { return this.options.position }
  trigger(name: string) { this.handlers.get(name)?.() }
}

class FakeMarkerCluster {
  static instances: FakeMarkerCluster[] = []
  setMap = vi.fn()
  renderedMarkers: FakeMarker[] = []
  setData = vi.fn((points: Array<Record<string, any>>) => { this.render(points) })

  constructor(
    public map: FakeMap,
    public points: Array<Record<string, any>>,
    public options: Record<string, any>,
  ) {
    FakeMarkerCluster.instances.push(this)
    this.render(points)
  }

  render(points: Array<Record<string, any>>) {
    this.renderedMarkers = points.map((point) => {
      const marker = new FakeMarker({ position: point.lnglat })
      this.options.renderMarker({ count: 1, marker, data: [point], indexs: [] })
      return marker
    })
  }
}

class FakeInfoWindow {
  static instances: FakeInfoWindow[] = []
  open = vi.fn()
  close = vi.fn()

  constructor(public options: Record<string, any>) {
    FakeInfoWindow.instances.push(this)
  }
}

class FakePixel {
  constructor(public x: number, public y: number) {}
}

const markers: EventMapMarker[] = [
  {
    event_id: 1,
    occurrence_id: 11,
    title: '上海 AI 交流会',
    summary: '团队活动',
    status: 'published',
    phase: 'upcoming',
    category: { id: 1, code: 'conference', name: '会议', color: '#DC2626', icon: '', sort_order: 1, enabled: true },
    fee_type: 'free',
    starts_at: '2026-07-18T08:00:00Z',
    venue_name: '上海市科技馆',
    city: '上海',
    district: '浦东新区',
    latitude: 31.22,
    longitude: 121.54,
  },
  {
    event_id: 2,
    occurrence_id: 22,
    title: 'AI Agent 工作坊',
    summary: '动手实践',
    status: 'published',
    phase: 'upcoming',
    category: { id: 2, code: 'workshop', name: '工作坊', color: '#D97706', icon: '', sort_order: 2, enabled: true },
    fee_type: 'free',
    starts_at: '2026-07-19T02:00:00Z',
    venue_name: '张江人工智能岛',
    city: '上海',
    district: '浦东新区',
    latitude: 31.2,
    longitude: 121.6,
  },
]

describe('EventMap', () => {
  beforeEach(() => {
    FakeMap.instances = []
    FakeMarker.instances = []
    FakeMarkerCluster.instances = []
    FakeInfoWindow.instances = []
    window.AMap = {
      Map: FakeMap,
      Marker: FakeMarker,
      MarkerCluster: FakeMarkerCluster,
      InfoWindow: FakeInfoWindow,
      Pixel: FakePixel,
    }
  })

  it('renders category-colored markers and fits the map to filtered results', async () => {
    const wrapper = mount(EventMap, {
      props: { apiKey: 'test-key', center: [31.23, 121.47], zoom: 11, markers, fitRequest: 1 },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    const rendered = FakeMarkerCluster.instances[0].renderedMarkers
    expect(rendered).toHaveLength(2)
    expect(rendered[0].content.style.getPropertyValue('--event-pin-color')).toBe('#DC2626')
    expect(FakeMarkerCluster.instances).toHaveLength(1)
    expect(FakeMap.instances[0].setFitView).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('events.map.markerCount:2')
  })

  it('links marker selection to a popup and detail action', async () => {
    const wrapper = mount(EventMap, {
      props: { apiKey: 'test-key', center: [31.23, 121.47], zoom: 11, markers, fitRequest: 1 },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    const rendered = FakeMarkerCluster.instances[0].renderedMarkers
    rendered[0].trigger('click')
    expect(wrapper.emitted('marker-select')?.[0]?.[0]).toMatchObject({ occurrence_id: 11 })
    expect(FakeInfoWindow.instances).toHaveLength(1)

    const popup = FakeInfoWindow.instances[0].options.content as HTMLElement
    expect(FakeInfoWindow.instances[0].options.autoMove).toBe(false)
    expect(popup.textContent).toContain('上海 AI 交流会')
    expect(popup.textContent).toContain('上海市科技馆')
    ;(popup.querySelector('.event-map-popup__details') as HTMLButtonElement).click()
    expect(wrapper.emitted('marker-details')?.[0]?.[0]).toMatchObject({ event_id: 1 })

    await wrapper.setProps({ selectedOccurrenceId: 11 })
    expect(FakeMarkerCluster.instances[0].renderedMarkers[0].content.classList.contains('event-map-pin--selected')).toBe(true)
    expect(FakeMap.instances[0].setZoomAndCenter).toHaveBeenCalledWith(14, expect.anything())
  })

  it('renders a count-based cluster marker', async () => {
    mount(EventMap, {
      props: { apiKey: 'test-key', center: [31.23, 121.47], zoom: 11, markers },
      global: { stubs: { Icon: true } },
    })
    await flushPromises()

    const clusterMarker = { setContent: vi.fn(), setOffset: vi.fn() }
    FakeMarkerCluster.instances[0].options.renderClusterMarker({ count: 12, marker: clusterMarker })
    const content = clusterMarker.setContent.mock.calls[0][0] as HTMLElement
    expect(content.className).toBe('event-map-cluster')
    expect(content.textContent).toBe('12')
    expect(content.getAttribute('aria-label')).toBe('events.map.clusterLabel:12')
  })
})
