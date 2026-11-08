import type { RouteRecordRaw } from 'vue-router'

/** Keep legacy links, saved query state and hashes working without changing APIs. */
export const workbenchRoutes: RouteRecordRaw[] = [
  {
    path: '/image-studio',
    redirect: (to) => ({ path: '/workbench/image', query: to.query, hash: to.hash }),
  },
  { path: '/workbench', redirect: (to) => ({ path: '/workbench/image', query: to.query, hash: to.hash }) },
  {
    path: '/workbench/image', name: 'ImageStudio',
    component: () => import('@/views/user/ImageStudioView.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, workspaceArea: 'workbench', workspaceContext: 'personal', title: 'Image Generation', titleKey: 'nav.imageStudio' },
  },
  {
    path: '/admin/workbench', name: 'AdminWorkbench',
    component: () => import('@/views/admin/workbench/WorkbenchOverviewView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Workbench Overview', titleKey: 'nav.workbenchOverview' },
  },
  {
    path: '/admin/workbench/tasks', name: 'AdminWorkbenchTasks',
    component: () => import('@/views/admin/workbench/WorkbenchTasksView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Generation Tasks', titleKey: 'nav.workbenchTasks' },
  },
  {
    path: '/admin/workbench/gallery', name: 'AdminWorkbenchGallery',
    component: () => import('@/views/admin/workbench/WorkbenchGalleryView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Cloud Gallery', titleKey: 'nav.workbenchGallery' },
  },
  {
    path: '/admin/workbench/settings', name: 'AdminWorkbenchSettings',
    component: () => import('@/views/admin/workbench/WorkbenchSettingsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Workbench Settings', titleKey: 'nav.workbenchSettings' },
  },
]
