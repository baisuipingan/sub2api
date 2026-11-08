import type { RouteLocationNormalizedLoaded, RouteLocationResolved, RouteRecordRaw } from 'vue-router'

export type WorkspaceArea = 'console' | 'workbench'
export type WorkspaceContext = 'admin' | 'personal'
export type WorkspaceRoute = Pick<RouteLocationNormalizedLoaded, 'path' | 'fullPath' | 'meta' | 'matched' | 'name' | 'params'>

export function workspaceLocation(route: Pick<WorkspaceRoute, 'path' | 'meta' | 'params'>, adminCustomIds: readonly string[] = []) {
  const area: WorkspaceArea = route.meta.workspaceArea ?? 'console'
  const context: WorkspaceContext = route.meta.requiresAdmin === true
    || (route.path.startsWith('/custom/') && adminCustomIds.includes(String(route.params.id)))
    ? 'admin' : route.meta.workspaceContext ?? 'personal'
  return { area, context }
}

export function workspaceKey(area: WorkspaceArea, context: WorkspaceContext) { return `${context}:${area}` }

export function workspaceDefault(area: WorkspaceArea, context: WorkspaceContext, isAdmin: boolean): string {
  if (context === 'admin' && isAdmin) return area === 'workbench' ? '/admin/workbench' : '/admin/dashboard'
  return area === 'workbench' ? '/workbench/image' : isAdmin ? '/keys' : '/dashboard'
}

/** Route state is authoritative; persisted memory stores only return destinations. */
export function withWorkspaceMeta(routes: RouteRecordRaw[]): RouteRecordRaw[] {
  return routes.map((route) => ({
    ...route,
    meta: route.meta?.requiresAuth === false ? route.meta : {
      workspaceArea: 'console',
      workspaceContext: route.meta?.requiresAdmin === true ? 'admin' : 'personal',
      ...route.meta,
    },
    ...(route.children ? { children: withWorkspaceMeta(route.children) } : {}),
  })) as RouteRecordRaw[]
}

export interface WorkspaceDestinationAccess {
  isAdmin: boolean
  backendModeEnabled: boolean
  isSimpleMode: boolean
  visiblePaths: readonly string[]
  adminCustomIds?: readonly string[]
}

/** Do not restore arbitrary storage URLs, hidden features, callbacks or another context. */
export function isWorkspaceDestination(
  route: RouteLocationResolved,
  area: WorkspaceArea,
  context: WorkspaceContext,
  access: WorkspaceDestinationAccess,
): boolean {
  if (!route.matched.length || route.meta.requiresAuth === false || route.redirectedFrom
    || route.path.startsWith('//') || !route.path.startsWith('/') || route.fullPath.length > 2048) return false
  if (context === 'admin' && !access.isAdmin) return false
  if (access.backendModeEnabled && !access.isAdmin) return false
  const location = workspaceLocation(route, access.adminCustomIds)
  if (location.area !== area || location.context !== context) return false
  if (access.isSimpleMode && ['/admin/subscriptions', '/admin/redeem', '/subscriptions', '/redeem']
    .some((path) => route.path === path || route.path.startsWith(path + '/'))) return false
  return access.visiblePaths.some((path) => route.path === path || route.path.startsWith(path + '/'))
}
