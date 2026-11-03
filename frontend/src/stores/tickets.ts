/**
 * 工单未读状态 Store
 *
 * 侧栏红点、列表页、管理端都需要同一个未读数。放在 pinia 里统一持有，
 * 避免各组件各请求一遍（既浪费请求，也会出现「侧栏有红点但列表没高亮」的不一致）。
 *
 * 轮询策略：只在已登录且页面可见时轮询，切到后台暂停，
 * 这样不会在用户挂着页面时持续打后台接口。
 */

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getUnreadCount, adminGetUnreadCount } from '@/api/tickets'

/** 轮询间隔。用户对工单回复的实时性要求不高，60 秒足够且足够省。 */
const POLL_INTERVAL_MS = 60_000

export const useTicketStore = defineStore('tickets', () => {
  const userUnread = ref(0)
  const adminUnread = ref(0)
  let timer: ReturnType<typeof setInterval> | null = null
  let started = false

  /** 侧栏用户红点数 */
  const userUnreadCount = computed(() => userUnread.value)
  /** 侧栏管理端红点数 */
  const adminUnreadCount = computed(() => adminUnread.value)

  /**
   * 刷新用户未读数。
   * 失败静默：红点不该因为一次网络抖动就弹错误打扰用户。
   */
  async function refreshUserUnread(): Promise<void> {
    try {
      const res = await getUnreadCount()
      userUnread.value = Number(res?.count ?? 0)
    } catch {
      // 忽略：保持上一次的值即可
    }
  }

  /** 刷新管理端未读数 */
  async function refreshAdminUnread(): Promise<void> {
    try {
      const res = await adminGetUnreadCount()
      adminUnread.value = Number(res?.count ?? 0)
    } catch {
      // 忽略
    }
  }

  /** 本地清零（例如刚看完列表），不必等下一次轮询 */
  function clearUserUnread(): void {
    userUnread.value = 0
  }

  function clearAdminUnread(): void {
    adminUnread.value = 0
  }

  /** 页面重新可见时立刻补一次，避免「切回来红点还是旧的」 */
  function onVisibilityChange(): void {
    if (typeof document === 'undefined') return
    if (document.visibilityState !== 'visible' || !started) return
    void refreshUserUnread()
    void refreshAdminUnread()
  }

  /**
   * 启动轮询。重复调用安全（幂等）。
   * @param isAdmin 管理员额外拉一次管理端未读
   */
  function startPolling(isAdmin: boolean): void {
    if (started) return
    started = true

    void refreshUserUnread()
    if (isAdmin) void refreshAdminUnread()

    timer = setInterval(() => {
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return
      void refreshUserUnread()
      if (isAdmin) void refreshAdminUnread()
    }, POLL_INTERVAL_MS)

    if (typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', onVisibilityChange)
    }
  }

  /** 停止轮询。登出时调用，避免请求带着失效 token 继续打。 */
  function stopPolling(): void {
    started = false
    if (timer) {
      clearInterval(timer)
      timer = null
    }
    if (typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
    userUnread.value = 0
    adminUnread.value = 0
  }

  return {
    userUnreadCount,
    adminUnreadCount,
    refreshUserUnread,
    refreshAdminUnread,
    clearUserUnread,
    clearAdminUnread,
    startPolling,
    stopPolling
  }
})
