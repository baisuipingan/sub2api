import { apiClient } from './client'

export type DesktopDownloadPlatform = 'win32-x64' | 'darwin-arm64'
export interface DesktopDownload {
  platform: DesktopDownloadPlatform
  available: boolean
  version?: string
  url?: string
  size?: number
  format?: string
}
export interface DesktopDownloads { platforms: DesktopDownload[] }

export async function getDesktopDownloads(): Promise<DesktopDownloads> {
  const { data } = await apiClient.get<DesktopDownloads>('/desktop/downloads')
  return data
}
