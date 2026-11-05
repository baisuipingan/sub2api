export type BrowserNotificationPermission = NotificationPermission | 'unsupported'
export type BrowserNotificationPermissionResult =
  | { ok: true }
  | { ok: false; reason: 'unsupported' | 'insecure' | 'denied' | 'default' | 'error'; error?: unknown }
export type BrowserNotificationReadiness =
  | { ok: true }
  | { ok: false; reason: 'unsupported' | 'insecure' | 'denied' | 'default' }

/** Stable for a signed-in user across token refreshes, different after logout/account switch. */
export function getBrowserNotificationSession(): string | null {
  try {
    const token = localStorage.getItem('auth_token')
    if (!token) return null
    const user = JSON.parse(localStorage.getItem('auth_user') ?? 'null') as { id?: unknown } | null
    return user && (typeof user.id === 'number' || typeof user.id === 'string')
      ? `user:${user.id}`
      : `session:${token}`
  } catch {
    return null
  }
}

export function isBrowserNotificationBackground() {
  return typeof document !== 'undefined' && (document.visibilityState === 'hidden' || !document.hasFocus())
}

function getNotificationConstructor() {
  if (typeof window === 'undefined' || !('Notification' in window)) return null
  return window.Notification
}

function isSecureNotificationContext() {
  return typeof window !== 'undefined' && window.isSecureContext
}

export function getBrowserNotificationReadiness(): BrowserNotificationReadiness {
  const NotificationConstructor = getNotificationConstructor()
  if (!NotificationConstructor) return { ok: false, reason: 'unsupported' }
  if (!isSecureNotificationContext()) return { ok: false, reason: 'insecure' }
  if (NotificationConstructor.permission === 'granted') return { ok: true }
  return { ok: false, reason: NotificationConstructor.permission }
}

export async function requestBrowserNotificationPermission(): Promise<BrowserNotificationPermissionResult> {
  const readiness = getBrowserNotificationReadiness()
  if (readiness.ok || readiness.reason !== 'default') return readiness

  try {
    const permission = await window.Notification.requestPermission()
    if (permission === 'granted') return { ok: true }
    if (permission === 'denied') return { ok: false, reason: 'denied' }
    return { ok: false, reason: 'default' }
  } catch (error) {
    return { ok: false, reason: 'error', error }
  }
}

export function showBrowserNotification(title: string, options?: NotificationOptions) {
  const NotificationConstructor = getNotificationConstructor()
  if (!NotificationConstructor || !isSecureNotificationContext() || NotificationConstructor.permission !== 'granted') return false

  try {
    const notification = new NotificationConstructor(title, {
      tag: 'task-completion',
      requireInteraction: false,
      ...options,
    })
    notification.onclick = () => {
      window.focus()
      notification.close()
    }
    return true
  } catch {
    return false
  }
}
