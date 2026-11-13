import { apiClient } from '../client'
import type { DesktopCredentialResult } from '../desktop'

/** 管理员操作精确到用户和托管Key；恢复在服务端轮换旧密钥。 */
export async function manageUserDesktopCredential(action: 'revoke' | 'restore', userId: number, keyId: number): Promise<DesktopCredentialResult> {
  const endpoint = action === 'restore' ? 'reset' : 'revoke'
  const { data } = await apiClient.post<DesktopCredentialResult>(`/admin/desktop/users/${userId}/credential/${endpoint}`, { key_id: keyId })
  return data
}
