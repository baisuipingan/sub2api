import { afterEach, describe, expect, it, vi } from 'vitest'
import { getBrowserNotificationSession, requestBrowserNotificationPermission, showBrowserNotification } from '../browserNotification'

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.removeItem('auth_token')
  localStorage.removeItem('auth_user')
})

function notificationAPI(permission: NotificationPermission) {
  const requestPermission = vi.fn().mockResolvedValue('granted')
  const constructor = vi.fn().mockImplementation(() => ({ close: vi.fn(), onclick: null }))
  Object.assign(constructor, { permission, requestPermission })
  vi.stubGlobal('Notification', constructor)
  vi.stubGlobal('isSecureContext', true)
  return { constructor, requestPermission }
}

describe('browser system notifications', () => {
  it('requests permission only from an undecided supported browser', async () => {
    const { requestPermission } = notificationAPI('default')
    await expect(requestBrowserNotificationPermission()).resolves.toEqual({ ok: true })
    expect(requestPermission).toHaveBeenCalledTimes(1)
  })

  it.each(['denied', 'default'] as const)('keeps a %s permission result explicit', async (permission) => {
    const { requestPermission } = notificationAPI('default')
    requestPermission.mockResolvedValueOnce(permission)
    await expect(requestBrowserNotificationPermission()).resolves.toEqual({ ok: false, reason: permission })
  })

  it('does not re-prompt denied permissions or create a notification', async () => {
    const { constructor, requestPermission } = notificationAPI('denied')
    await expect(requestBrowserNotificationPermission()).resolves.toEqual({ ok: false, reason: 'denied' })
    expect(showBrowserNotification('Finished')).toBe(false)
    expect(requestPermission).not.toHaveBeenCalled()
    expect(constructor).not.toHaveBeenCalled()
  })

  it('reports insecure contexts explicitly', async () => {
    notificationAPI('default')
    vi.stubGlobal('isSecureContext', false)
    await expect(requestBrowserNotificationPermission()).resolves.toEqual({ ok: false, reason: 'insecure' })
  })

  it('keeps user identity stable across token rotation but changes it on logout or account switch', () => {
    localStorage.setItem('auth_token', 'token-one')
    localStorage.setItem('auth_user', JSON.stringify({ id: 100 }))
    const session = getBrowserNotificationSession()
    localStorage.setItem('auth_token', 'token-two')
    expect(getBrowserNotificationSession()).toBe(session)
    localStorage.setItem('auth_user', JSON.stringify({ id: 200 }))
    expect(getBrowserNotificationSession()).not.toBe(session)
    localStorage.removeItem('auth_token')
    expect(getBrowserNotificationSession()).toBeNull()
  })
})
