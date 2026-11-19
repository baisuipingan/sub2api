import type { RouteRecordRaw } from 'vue-router'
import { workbenchChatApi } from '@/api/workbenchChat'

/** Keep legacy links, saved query state and hashes working without changing APIs. */
export const workbenchRoutes: RouteRecordRaw[] = [
  {
    path: '/image-studio',
    redirect: (to) => ({ path: '/workbench/image', query: to.query, hash: to.hash }),
  },
  { path: '/workbench', component: () => import('@/views/user/WorkbenchChatView.vue'), meta: { requiresAuth: true, workspaceArea: 'workbench', workspaceContext: 'personal' }, beforeEnter: async to => {
 let enabled = false
 try { enabled = (await workbenchChatApi.status()).enabled } catch { /* original image entry remains available */ }
 return { path: enabled ? '/workbench/chat' : '/workbench/image', query: to.query, hash: to.hash }
 } },
 {path:'/workbench/chat/:conversationId?',name:'WorkbenchChat',component:()=>import('@/views/user/WorkbenchChatView.vue'),meta:{requiresAuth:true,requiresAdmin:false,workspaceArea:'workbench',workspaceContext:'personal',title:'对话助手'}},
 {path:'/workbench/files',name:'WorkbenchFiles',component:()=>import('@/views/user/WorkbenchFilesView.vue'),meta:{requiresAuth:true,requiresAdmin:false,workspaceArea:'workbench',workspaceContext:'personal',title:'我的文件'}},
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
    path: '/admin/workbench/settings', name: 'AdminWorkbenchSettings',
    component: () => import('@/views/admin/workbench/WorkbenchSettingsView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, workspaceArea: 'workbench', workspaceContext: 'admin', title: 'Workbench Settings', titleKey: 'nav.workbenchSettings' },
  },
]
