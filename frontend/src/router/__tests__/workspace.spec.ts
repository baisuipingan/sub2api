import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { withWorkspaceMeta, workspaceLocation, isWorkspaceDestination } from '../workspace'
import { workbenchRoutes } from '../workbenchRoutes'

const blank = { template: '<div />' }
function router() {
  return createRouter({ history: createMemoryHistory(), routes: withWorkspaceMeta([
    ...workbenchRoutes.map((route) => route.redirect ? route : { ...route, component: blank }),
    { path: '/keys', component: blank, meta: { requiresAuth: true } },
    { path: '/admin/dashboard', component: blank, meta: { requiresAuth: true, requiresAdmin: true } },
    { path: '/custom/:id', component: blank, meta: { requiresAuth: true } },
    { path: '/login', component: blank, meta: { requiresAuth: false } },
  ]) })
}

describe('workspace route architecture', () => {
  it('redirects both old Studio links and the workbench root without losing query/hash', async () => {
    const r = router()
    await r.push('/image-studio?task=saved#preview')
    expect(r.currentRoute.value.fullPath).toBe('/workbench/image?task=saved#preview')
    expect(r.currentRoute.value.name).toBe('ImageStudio')
    expect(workspaceLocation(r.currentRoute.value)).toEqual({ area: 'workbench', context: 'personal' })
    await r.push('/workbench?model=gpt-image-2#prompt')
    expect(r.currentRoute.value.fullPath).toBe('/workbench/image?model=gpt-image-2#prompt')
  })

  it('keeps admin requirements on all three management views', () => {
    const r = router()
    for (const path of ['/admin/workbench', '/admin/workbench/tasks', '/admin/workbench/settings']) {
      const route = r.resolve(path)
      expect(route.meta.requiresAdmin).toBe(true)
      expect(workspaceLocation(route)).toEqual({ area: 'workbench', context: 'admin' })
    }
    expect(workspaceLocation(r.resolve('/keys'))).toEqual({ area: 'console', context: 'personal' })
    expect(workspaceLocation(r.resolve('/admin/dashboard'))).toEqual({ area: 'console', context: 'admin' })
  })

  it('does not infer personal use from the administrator role or expose admin custom menus as personal', () => {
    const r = router()
    expect(workspaceLocation(r.resolve('/workbench/image'))).toEqual({ area: 'workbench', context: 'personal' })
    expect(workspaceLocation(r.resolve('/custom/ops'), ['ops'])).toEqual({ area: 'console', context: 'admin' })
    expect(workspaceLocation(r.resolve('/custom/help'), ['ops'])).toEqual({ area: 'console', context: 'personal' })
  })

  it('only restores authenticated, currently visible, same-context destinations', () => {
    const r = router()
    const access = { isAdmin: true, backendModeEnabled: false, isSimpleMode: false, visiblePaths: ['/keys', '/admin/workbench'] }
    expect(isWorkspaceDestination(r.resolve('/keys?sort=name'), 'console', 'personal', access)).toBe(true)
    expect(isWorkspaceDestination(r.resolve('/keys'), 'console', 'admin', access)).toBe(false)
    expect(isWorkspaceDestination(r.resolve('/admin/workbench'), 'workbench', 'admin', { ...access, isAdmin: false })).toBe(false)
    expect(isWorkspaceDestination(r.resolve('/login'), 'console', 'personal', access)).toBe(false)
    expect(isWorkspaceDestination(r.resolve('/keys'), 'console', 'personal', { ...access, visiblePaths: [] })).toBe(false)
    expect(isWorkspaceDestination(r.resolve('/keys'), 'console', 'personal', { ...access, isAdmin: false, backendModeEnabled: true })).toBe(false)
  })
})
