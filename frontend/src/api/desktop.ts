import { apiClient } from './client'

export interface DesktopAuthorization {
  redirect_uri: string
  code_challenge: string
  code_challenge_method: 'S256'
  state: string
}

export async function authorizeDesktop(input: DesktopAuthorization): Promise<{ redirect_url: string }> {
  const { data } = await apiClient.post<{ redirect_url: string }>('/desktop/auth/authorize', input)
  return data
}

export interface DesktopCredentialResult { status: 'revoked' | 'active'; key_id: number }

export async function manageDesktopCredential(action: 'revoke' | 'reset', keyId: number): Promise<DesktopCredentialResult> {
  const { data } = await apiClient.post<DesktopCredentialResult>(`/desktop/credential/${action}`, { key_id: keyId })
  return data
}
