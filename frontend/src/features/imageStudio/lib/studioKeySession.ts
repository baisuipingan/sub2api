import { currentGalleryUserId } from './gallerySession'

interface StudioIdentity {
  token: string | null
  userId: number | null
}

let boundKey: { key: string; identity: StudioIdentity } | null = null

/** Claims are used only for immediate client-side isolation; the server verifies signatures. */
function studioTokenClaims(token: string | null): { userId: number; sid: string | null } | null {
  if (!token) return null
  try {
    const encoded = token.split('.')[1]
    if (!encoded) return null
    const payload = JSON.parse(atob(encoded.replace(/-/g, '+').replace(/_/g, '/'))) as { user_id?: unknown; sid?: unknown }
    const id = Number(payload.user_id)
    return Number.isSafeInteger(id) && id > 0
      ? { userId: id, sid: typeof payload.sid === 'string' && payload.sid ? payload.sid : null }
      : null
  } catch { return null }
}

export function studioTokenUserId(token: string | null): number | null {
  return studioTokenClaims(token)?.userId ?? null
}

export function isSameStudioLogin(first: string | null, second: string | null): boolean {
  const a = studioTokenClaims(first)
  const b = studioTokenClaims(second)
  return Boolean(a && b && a.sid && a.userId === b.userId && a.sid === b.sid)
}

export function captureStudioIdentity(): StudioIdentity {
  const token = localStorage.getItem('auth_token')
  const userId = currentGalleryUserId()
  const claimUserId = studioTokenUserId(token)
  return { token, userId: claimUserId !== null && claimUserId !== userId ? null : userId }
}

export function isStudioIdentityCurrent(identity: StudioIdentity): boolean {
  const current = captureStudioIdentity()
  return identity.token === current.token && identity.userId === current.userId
}

export function bindStudioApiKey(key: string, identity: StudioIdentity): void {
  if (key && identity.token && identity.userId && isStudioIdentityCurrent(identity)) {
    boundKey = { key, identity }
  }
}

export function isCurrentStudioApiKey(key: string): boolean {
  return Boolean(key && boundKey?.key === key && boundKey.identity.token
    && boundKey.identity.userId && isStudioIdentityCurrent(boundKey.identity))
}
