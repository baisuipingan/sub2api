import { getImageStudioDbScope } from './db'

let verifiedToken: string | null = null
let verifiedUserId: number | null = null

export function verifyGalleryIdentity(userId: number, token: string) {
  verifiedToken = token
  verifiedUserId = userId
}

export function isVerifiedGalleryAuth(token: string) {
  if (!verifiedToken || token !== verifiedToken || localStorage.getItem('auth_token') !== token) return false
  const userId = currentGalleryUserId()
  return userId === verifiedUserId && getImageStudioDbScope() === 'milorouter-ai-image-user-' + userId
}

export function currentGalleryUserId(): number | null {
  try {
    const id = Number((JSON.parse(localStorage.getItem('auth_user') ?? 'null') as { id?: unknown } | null)?.id)
    return Number.isSafeInteger(id) && id > 0 ? id : null
  } catch { return null }
}

export function mayReadGalleryCache() {
  return verifiedToken === null || isVerifiedGalleryAuth(localStorage.getItem('auth_token') ?? '')
}

/** Includes both the JWT and local user identity so a switch is noticed immediately. */
export function captureGallerySession() {
  return {
    token: localStorage.getItem('auth_token'),
    userId: currentGalleryUserId(),
    scope: getImageStudioDbScope(),
  }
}

export function isGallerySessionCurrent(session: ReturnType<typeof captureGallerySession>) {
  return session.token === localStorage.getItem('auth_token')
    && session.userId === currentGalleryUserId()
    && session.scope === getImageStudioDbScope()
}
