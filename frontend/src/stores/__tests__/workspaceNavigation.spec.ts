import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { useWorkspaceNavigationStore } from '../workspaceNavigation'
import { withWorkspaceMeta } from '@/router/workspace'

function router() {
  const component = { template: '<div />' }
  return createRouter({ history: createMemoryHistory(), routes: withWorkspaceMeta([
    { path: '/dashboard', component }, { path: '/keys', component }, { path: '/usage', component },
    { path: '/workbench/image', component, meta: { workspaceArea: 'workbench' } },
    { path: '/admin/dashboard', component, meta: { requiresAdmin: true } },
    { path: '/admin/workbench', component, meta: { requiresAdmin: true, workspaceArea: 'workbench' } },
    { path: '/login', component, meta: { requiresAuth: false } },
  ]) })
}

describe('workspace return destinations', () => {
  beforeEach(() => { sessionStorage.clear(); setActivePinia(createPinia()) })

  it('remembers management and personal return routes independently', async () => {
    const r = router()
    const nav = useWorkspaceNavigationStore()
    nav.syncUser(7)
    for (const path of ['/usage?period=month', '/admin/dashboard', '/workbench/image']) {
      await r.push(path); nav.remember(r.currentRoute.value)
    }
    const access = { isAdmin: true, isSimpleMode: false, backendModeEnabled: false, visiblePaths: ['/usage', '/admin/dashboard', '/workbench/image'] }
    expect(nav.destination(r, 'console', 'personal', access)).toBe('/usage?period=month')
    expect(nav.destination(r, 'console', 'admin', access)).toBe('/admin/dashboard')
    expect(nav.destination(r, 'workbench', 'personal', access)).toBe('/workbench/image')
  })

  it('revalidates feature visibility and survives a same-user tab reload', async () => {
    const r = router()
    const nav = useWorkspaceNavigationStore()
    nav.syncUser(7)
    await r.push('/usage'); nav.remember(r.currentRoute.value)
    nav.saveScroll('personal:console', 210)
    setActivePinia(createPinia())
    const restored = useWorkspaceNavigationStore(); restored.syncUser(7)
    expect(restored.paths['personal:console']).toBe('/usage')
    expect(restored.scrollPositions['personal:console']).toBe(210)
    expect(restored.destination(r, 'console', 'personal', { isAdmin: false, isSimpleMode: false, backendModeEnabled: false, visiblePaths: ['/keys'] })).toBe('/dashboard')
  })

  it('clears account-specific destinations and scroll state on logout or account change', async () => {
    const r = router(); const nav = useWorkspaceNavigationStore(); nav.syncUser(7)
    await r.push('/usage'); nav.remember(r.currentRoute.value); nav.saveScroll('personal:console', 200)
    nav.syncUser(8)
    expect(nav.paths).toEqual({}); expect(nav.scrollPositions).toEqual({})
    nav.syncUser(null)
    expect(JSON.parse(sessionStorage.getItem('milorouter_workspace_navigation_v1')!).userId).toBeNull()
  })

  it('discards malformed or external saved navigation and does not remember public routes', async () => {
    sessionStorage.setItem('milorouter_workspace_navigation_v1', JSON.stringify({ userId: 7, paths: { 'personal:console': '//evil.test', 'admin:console': 'https://evil.test' }, scrollPositions: { 'personal:console': -5 } }))
    const r = router(); const nav = useWorkspaceNavigationStore(); nav.syncUser(7)
    expect(nav.paths).toEqual({}); expect(nav.scrollPositions).toEqual({})
    await r.push('/login'); nav.remember(r.currentRoute.value)
    expect(nav.paths).toEqual({})
  })
})
