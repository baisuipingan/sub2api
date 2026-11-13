import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { post } }))
import { manageUserDesktopCredential } from '@/api/admin/desktopCredentials'

describe('admin Agent credential actions', () => {
  beforeEach(() => post.mockReset())
  it.each([
    ['revoke', 'revoke', 'revoked'],
    ['restore', 'reset', 'active'],
  ] as const)('targets the exact user and key for %s', async (action, endpoint, status) => {
    const result = { status, key_id: 73 }
    post.mockResolvedValueOnce({ data: result })
    await expect(manageUserDesktopCredential(action, 8, 73)).resolves.toBe(result)
    expect(post).toHaveBeenCalledWith(`/admin/desktop/users/8/credential/${endpoint}`, { key_id: 73 })
  })
  it('leaves step-up and forbidden decisions to the caller', async () => {
    const error = { status: 403, code: 'STEP_UP_REQUIRED' }
    post.mockRejectedValueOnce(error)
    await expect(manageUserDesktopCredential('restore', 8, 73)).rejects.toBe(error)
    expect(post).toHaveBeenCalledTimes(1)
  })
})
