import type { RouteRecordRaw } from 'vue-router'
import type { WorkspaceArea, WorkspaceContext } from './workspace'

export function desktopDownloadPath(area: WorkspaceArea, context: WorkspaceContext): string {
  return `${context === 'admin' ? '/admin' : ''}${area === 'workbench' ? '/workbench' : ''}/downloads`
}

// One page with explicit route metadata keeps the current area and role context
// intact, including when a download link is opened directly or in another tab.
export const desktopDownloadRoutes: RouteRecordRaw[] = (['console', 'workbench'] as const).flatMap(area =>
  (['personal', 'admin'] as const).map(context => ({
    path: desktopDownloadPath(area, context),
    name: `DesktopDownloads-${context}-${area}`,
    component: () => import('@/views/user/DesktopDownloadsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: context === 'admin',
      workspaceArea: area,
      workspaceContext: context,
      title: 'MiloCode Downloads',
      titleKey: 'desktop.downloads.title',
    },
  })),
)
