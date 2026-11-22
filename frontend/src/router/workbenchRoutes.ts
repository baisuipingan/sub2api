import type { RouteRecordRaw } from 'vue-router'
import { workbenchChatApi } from '@/api/workbenchChat'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import { workspaceDefault } from './workspace'

async function workbenchLanding(imageEnabled: boolean): Promise<string> {
  const status = await workbenchChatApi.status().catch(() => null)
  if (status?.enabled) return '/workbench/chat'
  if (imageEnabled) return '/workbench/image'
  if (status?.files_enabled) return '/workbench/files'
  return workspaceDefault('console', 'personal', useAuthStore().isAdmin)
}

/** Keep legacy links, saved query state and hashes working without changing APIs. */
export const workbenchRoutes: RouteRecordRaw[] = [
  {
    path: '/image-studio',
    redirect: (to) => ({ path: '/workbench/image', query: to.query, hash: to.hash }),
  },
  {
    path: '/workbench',
    component: () => import('@/views/user/WorkbenchChatView.vue'),
    meta: { requiresAuth: true, workspaceArea: 'workbench', workspaceContext: 'personal' },
    beforeEnter: async to => {
      const app = useAppStore()
      const settings = await app.fetchPublicSettings()
      const imageEnabled = resolveFeatureFlag(settings ?? app.cachedPublicSettings, FeatureFlags.imageStudio)
      const path = await workbenchLanding(imageEnabled)
      return path.startsWith('/workbench/') ? { path, query: to.query, hash: to.hash } : { path }
    },
  },
 {path:'/workbench/chat/:conversationId?',name:'WorkbenchChat',component:()=>import('@/views/user/WorkbenchChatView.vue'),meta:{requiresAuth:true,requiresAdmin:false,workspaceArea:'workbench',workspaceContext:'personal',title:'对话助手'}},
 {path:'/workbench/files',name:'WorkbenchFiles',component:()=>import('@/views/user/WorkbenchFilesView.vue'),meta:{requiresAuth:true,requiresAdmin:false,workspaceArea:'workbench',workspaceContext:'personal',title:'我的文件'}},
  {
    path: '/workbench/image', name: 'ImageStudio',
    component: () => import('@/views/user/ImageStudioView.vue'),
    meta: { requiresAuth: true, requiresAdmin: false, workspaceArea: 'workbench', workspaceContext: 'personal', title: 'Image Generation', titleKey: 'nav.imageStudio' },
    beforeEnter: async () => {
      const app = useAppStore()
      const settings = await app.fetchPublicSettings(true)
      if (!resolveFeatureFlag(settings ?? app.cachedPublicSettings, FeatureFlags.imageStudio)) {
        return { path: await workbenchLanding(false), replace: true }
      }
    },
  },
  {
    path: '/admin/workbench', name: 'AdminWorkbench',
    component: () => import('@/views/admin/workbench/WorkbenchOverviewView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Workbench Overview', titleKey: 'nav.workbenchOverview' },
  },
  {
    path: '/admin/workbench/chat', name: 'AdminWorkbenchChat',
    component: () => import('@/views/admin/workbench/WorkbenchChatRunsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: '对话记录' },
  },
  {
    path: '/admin/workbench/files', name: 'AdminWorkbenchFiles',
    component: () => import('@/views/admin/workbench/WorkbenchResourcesView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: '用户文件' },
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
    path: '/admin/workbench/office-nodes', name: 'AdminWorkbenchOfficeNodes',
    component: () => import('@/views/admin/workbench/WorkbenchOfficeNodesView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Office Compute Nodes', titleKey: 'nav.workbenchOfficeNodes' },
  },
  {
    path: '/admin/workbench/settings', name: 'AdminWorkbenchSettings',
    component: () => import('@/views/admin/workbench/WorkbenchSettingsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Workbench Settings', titleKey: 'nav.workbenchSettings' },
  },
]
