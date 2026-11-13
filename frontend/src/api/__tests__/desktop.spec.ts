import { beforeEach, describe, expect, it, vi } from 'vitest'
const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))
import { authorizeDesktop, manageDesktopCredential } from '@/api/desktop'

describe('desktop browser and credential API', () => {
  beforeEach(() => post.mockReset())
  it('authorizes only the explicit consent payload on the authenticated endpoint', async () => {
    const payload = { redirect_uri: 'http://127.0.0.1:43210/callback', code_challenge: 'a'.repeat(43), code_challenge_method: 'S256' as const, state: 'b'.repeat(43) }
    const data = { redirect_url: `${payload.redirect_uri}?code=opaque` }
    post.mockResolvedValueOnce({ data })
    await expect(authorizeDesktop(payload)).resolves.toBe(data)
    expect(post).toHaveBeenCalledWith('/desktop/auth/authorize', payload)
  })
  it.each(['revoke', 'reset'] as const)('binds %s to the displayed credential instead of guessing by name', async action => {
    const data = { status: action === 'revoke' ? 'revoked' : 'active', key_id: 42 }
    post.mockResolvedValueOnce({ data })
    await expect(manageDesktopCredential(action, 42)).resolves.toBe(data)
    expect(post).toHaveBeenCalledWith(`/desktop/credential/${action}`, { key_id: 42 })
  })
})
