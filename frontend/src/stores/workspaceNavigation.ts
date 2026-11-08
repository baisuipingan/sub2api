import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Router } from 'vue-router'
import { isWorkspaceDestination, workspaceDefault, workspaceKey, workspaceLocation, type WorkspaceArea, type WorkspaceContext, type WorkspaceDestinationAccess, type WorkspaceRoute } from '@/router/workspace'

const STORAGE_KEY = 'milorouter_workspace_navigation_v1'
const MEMORY_KEYS = ['admin:console', 'admin:workbench', 'personal:console', 'personal:workbench']

export const useWorkspaceNavigationStore = defineStore('workspaceNavigation', () => {
  const userId = ref<number | null>(null)
  const paths = ref<Record<string, string>>({})
  const scrollPositions = ref<Record<string, number>>({})

  function persist() {
    try { sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ userId: userId.value, paths: paths.value, scrollPositions: scrollPositions.value })) } catch { /* private mode */ }
  }

  function syncUser(nextUserId: number | null) {
    if (userId.value === nextUserId && nextUserId !== null) return
    userId.value = nextUserId
    paths.value = {}
    scrollPositions.value = {}
    try {
      const saved = JSON.parse(sessionStorage.getItem(STORAGE_KEY) ?? 'null')
      if (nextUserId !== null && saved?.userId === nextUserId) {
        for (const key of MEMORY_KEYS) {
          const path = saved.paths?.[key]
          if (typeof path === 'string' && path.startsWith('/') && !path.startsWith('//') && path.length <= 2048) paths.value[key] = path
          const position = saved.scrollPositions?.[key]
          if (Number.isFinite(position) && position >= 0) scrollPositions.value[key] = position
        }
      }
    } catch { /* unknown data is discarded */ }
    persist()
  }

  function remember(route: WorkspaceRoute, adminCustomIds: readonly string[] = []) {
    if (!userId.value || !route.matched.length || route.meta.requiresAuth === false) return
    if (route.path.startsWith('/auth/') || route.path.startsWith('/payment/')) return
    const { area, context } = workspaceLocation(route, adminCustomIds)
    paths.value[workspaceKey(area, context)] = route.fullPath
    persist()
  }

  function destination(router: Router, area: WorkspaceArea, context: WorkspaceContext, access: WorkspaceDestinationAccess) {
    const saved = paths.value[workspaceKey(area, context)]
    if (saved) {
      try {
        const resolved = router.resolve(saved)
        if (isWorkspaceDestination(resolved, area, context, access)) return saved
      } catch { /* invalid tab storage must not break the sidebar */ }
    }
    return workspaceDefault(area, context, access.isAdmin)
  }

  function saveScroll(key: string, position: number) {
    if (!userId.value || !MEMORY_KEYS.includes(key) || !Number.isFinite(position)) return
    scrollPositions.value[key] = Math.max(0, position)
    persist()
  }

  return { userId, paths, scrollPositions, syncUser, remember, destination, saveScroll }
})
