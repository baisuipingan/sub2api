import { apiClient } from '../client'

export interface DesktopReleaseStorageDraft {
  bucket: string
  public_base_url: string
}

/** The release bucket inherits backup credentials server-side; secrets never enter this form. */
export interface DesktopReleaseStorageConfig extends DesktopReleaseStorageDraft {
  release_base_url: string
  backup_s3_configured: boolean
  configured: boolean
}

const path = '/admin/desktop/release-storage'

export async function getDesktopReleaseStorage(): Promise<DesktopReleaseStorageConfig> {
  const { data } = await apiClient.get<DesktopReleaseStorageConfig>(path)
  return data
}

export async function updateDesktopReleaseStorage(draft: DesktopReleaseStorageDraft): Promise<DesktopReleaseStorageConfig> {
  const { data } = await apiClient.put<DesktopReleaseStorageConfig>(path, draft)
  return data
}

export async function testDesktopReleaseStorage(draft: DesktopReleaseStorageDraft): Promise<{ success: boolean }> {
  const { data } = await apiClient.post<{ success: boolean }>(`${path}/test`, draft)
  return data
}
