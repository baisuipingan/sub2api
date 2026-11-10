import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { announcementsAPI } from '@/api'
import type { UserAnnouncement } from '@/types'
import type { AnnouncementCenterTab } from '@/api/announcements'

const THROTTLE_MS = 20 * 60 * 1000

export const useAnnouncementStore = defineStore('announcements', () => {
  // Timeline pages and the current notices are independent from the complete
  // unread count. A long timeline must never truncate the bell's badge.
  const announcements = ref<UserAnnouncement[]>([])
  const notices = ref<UserAnnouncement[]>([])
  const loading = ref(false)
  const centerOpen = ref(false)
  const centerTab = ref<AnnouncementCenterTab>('notice')
  const noticeLoading = ref(false)
  const timelineLoading = ref(false)
  const noticeError = ref(false)
  const timelineError = ref(false)
  const noticePage = ref(0)
  const timelinePage = ref(0)
  const noticePages = ref(0)
  const timelinePages = ref(0)
  const noticeCount = ref(0)
  const timelineCount = ref(0)
  const markingAll = ref(false)
  const readingIds = ref<number[]>([])
  const currentPopup = ref<UserAnnouncement | null>(null)
  const popupQueue = ref<UserAnnouncement[]>([])
  const snapshot = ref('')
  const serverUnreadCount = ref(0)
  const unreadCount = computed(() => serverUnreadCount.value)

  let lastFetchTime = 0
  let session = 0
  let summaryGeneration = 0
  let readRevision = 0
  let shownPopupIds = new Set<number>()
  let readOverrides = new Map<number, string>()
  const pageGenerations: Record<AnnouncementCenterTab, number> = { notice: 0, timeline: 0 }
  const pageControllers: Partial<Record<AnnouncementCenterTab, AbortController>> = {}
  let summaryController: AbortController | undefined

  const isTimeline = (item: UserAnnouncement) => (item.display_mode ?? 'timeline') === 'timeline'
  const withReadState = (item: UserAnnouncement): UserAnnouncement => readOverrides.has(item.id)
    ? { ...item, read_at: readOverrides.get(item.id) } : item
  const allKnown = () => [...announcements.value, ...notices.value, ...popupQueue.value, ...(currentPopup.value ? [currentPopup.value] : [])]

  function showNextPopup() {
    if (centerOpen.value || currentPopup.value) return
    while (popupQueue.value.length) {
      const next = popupQueue.value.shift()!
      if (next.read_at || readOverrides.has(next.id) || shownPopupIds.has(next.id)) continue
      currentPopup.value = next
      shownPopupIds.add(next.id)
      return
    }
  }

  function updatePopupQueue(items: UserAnnouncement[]) {
    const currentId = currentPopup.value?.id
    popupQueue.value = items.map(withReadState).filter(item => item.notify_mode === 'popup'
      && !item.read_at && !shownPopupIds.has(item.id) && item.id !== currentId)
    if (currentPopup.value && !items.some(item => item.id === currentId)) currentPopup.value = null
    showNextPopup()
  }

  /** Login/route/visibility hooks keep their existing quiet summary refresh. */
  async function fetchAnnouncements(force = false) {
    const now = Date.now()
    if (!force && lastFetchTime && now - lastFetchTime < THROTTLE_MS) return
    lastFetchTime = now
    const currentSession = session
    const generation = ++summaryGeneration
    const revision = readRevision
    summaryController?.abort()
    const controller = new AbortController()
    summaryController = controller
    loading.value = true
    try {
      const result = await announcementsAPI.summary(controller.signal)
      if (currentSession !== session || generation !== summaryGeneration) return
      noticeCount.value = result.notice_count
      timelineCount.value = result.timeline_count
      // A read committed while this request was running wins over its stale
      // response. The next refresh obtains a fresh count and snapshot together.
      if (revision === readRevision) {
        serverUnreadCount.value = result.timeline_unread_count
        snapshot.value = result.snapshot
      } else lastFetchTime = 0
      updatePopupQueue(result.popup_candidates)
      return true
    } catch (error) {
      if (currentSession !== session || generation !== summaryGeneration) return
      lastFetchTime = 0
      if (!controller.signal.aborted) console.error('Failed to fetch announcements:', error)
      return false
    } finally {
      if (currentSession === session && generation === summaryGeneration) loading.value = false
    }
  }

  async function loadCenterPage(tab: AnnouncementCenterTab, append = false) {
    const busy = tab === 'notice' ? noticeLoading : timelineLoading
    const failed = tab === 'notice' ? noticeError : timelineError
    const page = tab === 'notice' ? noticePage : timelinePage
    const pages = tab === 'notice' ? noticePages : timelinePages
    const items = tab === 'notice' ? notices : announcements
    if (append && (busy.value || page.value >= pages.value)) return
    const currentSession = session
    const generation = ++pageGenerations[tab]
    pageControllers[tab]?.abort()
    const controller = new AbortController()
    pageControllers[tab] = controller
    busy.value = true
    failed.value = false
    const nextPage = append ? page.value + 1 : 1
    try {
      const result = await announcementsAPI.center(tab, nextPage, controller.signal)
      if (currentSession !== session || generation !== pageGenerations[tab]) return false
      const incoming = result.items.map(withReadState)
      if (append) {
        const merged = new Map(items.value.map(item => [item.id, item]))
        for (const item of incoming) merged.set(item.id, item)
        items.value = [...merged.values()]
      } else items.value = incoming
      page.value = result.page
      pages.value = result.pages
      if (tab === 'notice') noticeCount.value = result.total
      else timelineCount.value = result.total
      return true
    } catch (error) {
      if (currentSession !== session || generation !== pageGenerations[tab]) return false
      if (!controller.signal.aborted) failed.value = true
      return false
    } finally {
      if (currentSession === session && generation === pageGenerations[tab]) busy.value = false
    }
  }

  async function openCenter() {
    centerOpen.value = true
    centerTab.value = 'notice'
    pageGenerations.timeline++
    pageControllers.timeline?.abort()
    timelineLoading.value = false
    timelineError.value = false
    timelinePage.value = 0
    timelinePages.value = 0
    announcements.value = []
    // A passive refresh can queue a popup while the user is reading the center,
    // but it must never overlay or mark the center's contents as read.
    await Promise.all([fetchAnnouncements(true), loadCenterPage('notice')])
  }
  function closeCenter() {
    centerOpen.value = false
    showNextPopup()
  }
  async function selectCenterTab(tab: AnnouncementCenterTab) {
    centerTab.value = tab
    if ((tab === 'notice' ? noticePage.value : timelinePage.value) === 0) await loadCenterPage(tab)
  }

  function applyRead(ids: number[], at: string) {
    const set = new Set(ids)
    for (const id of ids) readOverrides.set(id, at)
    for (const collection of [announcements.value, notices.value, popupQueue.value]) {
      for (const item of collection) if (set.has(item.id)) item.read_at = at
    }
    popupQueue.value = popupQueue.value.filter(item => !set.has(item.id))
    if (currentPopup.value && set.has(currentPopup.value.id)) currentPopup.value = null
  }

  async function markAsRead(id: number) {
    if (markingAll.value || readingIds.value.includes(id)) return false
    const currentSession = session
    const known = allKnown().find(item => item.id === id)
    const counted = known && isTimeline(known) && !known.read_at && !readOverrides.has(id)
    readingIds.value = [...readingIds.value, id]
    try {
      await announcementsAPI.markRead(id)
      if (currentSession !== session) return false
      readRevision++
      applyRead([id], new Date().toISOString())
      if (counted) serverUnreadCount.value = Math.max(0, serverUnreadCount.value - 1)
      // Other-device reads and newly published content are reflected by the
      // server's complete summary, never guessed from the loaded page.
      void fetchAnnouncements(true)
      showNextPopup()
      return true
    } catch (error) {
      if (currentSession === session) console.error('Failed to mark announcement as read:', error)
      return false
    } finally {
      if (currentSession === session) readingIds.value = readingIds.value.filter(value => value !== id)
    }
  }

  async function dismissPopup() {
    const item = currentPopup.value
    if (!item) return false
    return markAsRead(item.id)
  }

  async function markAllAsRead() {
    if (markingAll.value || readingIds.value.length || !snapshot.value || !unreadCount.value) return
    const currentSession = session
    const token = snapshot.value
    markingAll.value = true
    try {
      const result = await announcementsAPI.markAllRead(token)
      if (currentSession !== session) return
      readRevision++
      applyRead(result.marked_ids, new Date().toISOString())
      serverUnreadCount.value = result.summary.timeline_unread_count
      snapshot.value = result.summary.snapshot
      noticeCount.value = result.summary.notice_count
      timelineCount.value = result.summary.timeline_count
      updatePopupQueue(result.summary.popup_candidates)
      showNextPopup()
    } catch (error) {
      if (currentSession === session && (error as { response?: { data?: { reason?: string } } })?.response?.data?.reason === 'ANNOUNCEMENT_SNAPSHOT_INVALID') {
        // Refresh the token, but never re-submit a wider snapshot on behalf of
        // the user. A second explicit confirmation is required.
        await fetchAnnouncements(true)
      }
      throw error
    } finally {
      if (currentSession === session) markingAll.value = false
    }
  }

  function reset() {
    session++
    summaryGeneration++
    summaryController?.abort()
    for (const tab of ['notice', 'timeline'] as const) {
      pageGenerations[tab]++
      pageControllers[tab]?.abort()
    }
    announcements.value = []
    notices.value = []
    lastFetchTime = 0
    shownPopupIds = new Set()
    readOverrides = new Map()
    readRevision = 0
    popupQueue.value = []
    currentPopup.value = null
    centerOpen.value = false
    centerTab.value = 'notice'
    snapshot.value = ''
    serverUnreadCount.value = 0
    loading.value = noticeLoading.value = timelineLoading.value = false
    noticeError.value = timelineError.value = false
    noticePage.value = timelinePage.value = noticePages.value = timelinePages.value = 0
    noticeCount.value = timelineCount.value = 0
    markingAll.value = false
    readingIds.value = []
  }

  return {
    announcements, notices, loading, centerOpen, centerTab, noticeLoading, timelineLoading,
    noticeError, timelineError, noticePage, timelinePage, noticePages, timelinePages,
    noticeCount, timelineCount, snapshot, markingAll, readingIds, currentPopup, unreadCount,
    fetchAnnouncements, loadCenterPage, openCenter, closeCenter, selectCenterTab,
    dismissPopup, markAsRead, markAllAsRead, reset
  }
})
