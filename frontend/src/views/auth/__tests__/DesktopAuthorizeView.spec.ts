import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DesktopAuthorizeView from '../DesktopAuthorizeView.vue'

const { auth, authorize, routeState, assign } = vi.hoisted(() => ({
  auth: { user: { id: 1, email: 'user@example.test' } }, authorize: vi.fn(), assign: vi.fn(),
  routeState: { current: {} as { fullPath: string; query: Record<string, unknown> } },
}))
vi.mock('vue-router', () => ({ useRoute: () => routeState.current }))
vi.mock('@/api/desktop', () => ({ authorizeDesktop: authorize }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const request = { client_id: 'zcode-desktop', response_type: 'code', redirect_uri: 'http://127.0.0.1:43210/callback', code_challenge: 'a'.repeat(43), code_challenge_method: 'S256', state: 'b'.repeat(43) }
const result = `${request.redirect_uri}?code=${'c'.repeat(43)}&state=${request.state}`
const originalLocation = window.location
const mounted: ReturnType<typeof mount>[] = []
function render() {
  const wrapper = mount(DesktopAuthorizeView, { global: { stubs: { Icon: true, RouterLink: true } } })
  mounted.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.clearAllMocks()
  routeState.current = reactive({ fullPath: '/desktop/authorize?request=1', query: { ...request } })
  authorize.mockResolvedValue({ redirect_url: result })
  Object.defineProperty(window, 'location', { configurable: true, value: { ...originalLocation, assign } })
})
afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
})

describe('desktop browser consent', () => {
  it('shows the account and local return address and waits for explicit consent', async () => {
    const wrapper = render()
    expect(wrapper.get('[data-test="desktop-client"]').text()).toBe('MiloCode')
    expect(wrapper.text()).not.toContain('ZCode')
    expect(wrapper.text()).not.toContain(request.code_challenge)
    expect(wrapper.text()).not.toContain(request.state)
    expect(wrapper.text()).toContain(auth.user.email)
    expect(wrapper.get('[data-test="desktop-callback"]').text()).toBe(request.redirect_uri)
    expect(authorize).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    const { client_id: _client, response_type: _response, ...body } = request
    expect(authorize).toHaveBeenCalledWith(body)
    expect(assign).toHaveBeenCalledWith(result)
  })
  it('returns a validated cancellation immediately without generating an authorization code', async () => {
    const wrapper = render()
    await wrapper.get('[data-test="desktop-deny"]').trigger('click')
    expect(wrapper.get('[role="status"]').text()).toBe('desktop.cancelled')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(authorize).not.toHaveBeenCalled()
    expect(assign).toHaveBeenCalledWith(`${request.redirect_uri}?error=access_denied&state=${request.state}`)
  })
  it('does not render approval for an untrusted callback or duplicate query', () => {
    routeState.current.query.redirect_uri = ['http://127.0.0.1:43210/callback', 'https://attacker.test']
    const wrapper = render()
    expect(wrapper.get('[role="alert"]').text()).toBe('desktop.invalidRequest')
    expect(wrapper.find('form').exists()).toBe(false)
    expect(authorize).not.toHaveBeenCalled()
  })
  it('reports safe failure text, keeps consent retryable and does not leak server details', async () => {
    authorize.mockRejectedValueOnce({ message: 'private database connection error' })
    const wrapper = render()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('desktop.authorizeFailed')
    expect(wrapper.text()).not.toContain('private database')
    expect(assign).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(assign).toHaveBeenCalledWith(result)
  })
  it('rejects a server callback with a different state', async () => {
    authorize.mockResolvedValueOnce({ redirect_url: result.replace(request.state, 'x'.repeat(43)) })
    const wrapper = render()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('desktop.invalidRedirect')
    expect(assign).not.toHaveBeenCalled()
  })
  it.each(['route change', 'unmount'])('ignores a late authorization after %s', async cause => {
    let resolve!: (value: { redirect_url: string }) => void
    authorize.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = render()
    await wrapper.get('form').trigger('submit')
    if (cause === 'unmount') wrapper.unmount()
    else {
      routeState.current.query = { ...request, state: 'd'.repeat(43) }
      routeState.current.fullPath = '/desktop/authorize?request=2'
      await flushPromises()
    }
    resolve({ redirect_url: result })
    await flushPromises()
    expect(assign).not.toHaveBeenCalled()
  })
})
