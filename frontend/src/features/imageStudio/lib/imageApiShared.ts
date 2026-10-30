import type { AppSettings, ImageApiKind, ImageApiResponse, TaskParams } from '../types'
import { blobToDataUrl } from './dataUrl'

export const MIME_MAP: Record<string, string> = {
  png: 'image/png',
  jpeg: 'image/jpeg',
  webp: 'image/webp',
}

export const MAX_MASK_EDIT_FILE_BYTES = 50 * 1024 * 1024
export const MAX_IMAGE_INPUT_PAYLOAD_BYTES = 512 * 1024 * 1024

export interface CallApiOptions {
  settings: AppSettings
  modelId?: string
  apiKind?: ImageApiKind
  prompt: string
  params: TaskParams
  inputImageDataUrls: string[]
  maskDataUrl?: string
  signal?: AbortSignal
  nativeTransparentOutput?: boolean
  streamingGeneration?: boolean
  streamingEdit?: boolean
  onPartialImage?: (partial: { image: string; partialIndex: number; requestIndex?: number }) => void
}

export interface ImageGenerateResult {
  images: string[]
  actualParams?: Partial<TaskParams>
  actualParamsList?: Array<Partial<TaskParams> | undefined>
  revisedPrompts?: Array<string | undefined>
  rawImageUrls?: string[]
  failedRequests?: Array<{ requestIndex: number; error: string }>
}

export type CallApiResult = ImageGenerateResult

export function isHttpUrl(value: unknown): value is string {
  return typeof value === 'string' && /^https?:\/\//i.test(value)
}

export function normalizeBase64Image(value: string, fallbackMime: string) {
  const match = value.match(/^data:([^;,]+);base64,([\s\S]*)$/i)
  const payload = (match?.[2] ?? value).replace(/\s/g, '')
  const mime = detectBase64ImageMime(payload) ?? match?.[1] ?? fallbackMime
  return `data:${mime};base64,${payload}`
}

export function detectBase64ImageMime(payload: string) {
  try {
    const bytes = Uint8Array.from(atob(payload.replace(/\s/g, '').slice(0, 32)), (character) => character.charCodeAt(0))
    if (bytes.length >= 8 && bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47
      && bytes[4] === 0x0d && bytes[5] === 0x0a && bytes[6] === 0x1a && bytes[7] === 0x0a) return 'image/png'
    if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return 'image/jpeg'
    if (bytes.length >= 12 && String.fromCharCode(...bytes.slice(0, 4)) === 'RIFF'
      && String.fromCharCode(...bytes.slice(8, 12)) === 'WEBP') return 'image/webp'
  } catch {
    // 不是可解码的 Base64 时沿用接口声明的格式。
  }
  return null
}

export async function fetchImageUrlAsDataUrl(url: string, fallbackMime: string, signal?: AbortSignal) {
  if (url.startsWith('data:')) return url

  try {
    const response = await fetch(url, { cache: 'no-store', signal })
    if (!response.ok) throw new Error(`图片读取失败：HTTP ${response.status}`)
    return blobToDataUrl(await response.blob(), fallbackMime)
  } catch (error) {
    if (error instanceof Error && error.message.startsWith('图片读取失败')) throw error
    throw new Error('图片链接无法读取，请稍后重试')
  }
}

export function getDataUrlDecodedByteSize(dataUrl: string) {
  const commaIndex = dataUrl.indexOf(',')
  if (commaIndex < 0) return dataUrl.length
  const metadata = dataUrl.slice(0, commaIndex)
  const payload = dataUrl.slice(commaIndex + 1)
  if (!/;base64/i.test(metadata)) return decodeURIComponent(payload).length
  const normalized = payload.replace(/\s/g, '')
  const padding = normalized.endsWith('==') ? 2 : normalized.endsWith('=') ? 1 : 0
  return Math.max(0, Math.floor((normalized.length * 3) / 4) - padding)
}

export function assertImageInputPayloadSize(bytes: number) {
  if (bytes > MAX_IMAGE_INPUT_PAYLOAD_BYTES) throw new Error('参考图片总大小超过限制')
}

export function assertMaskEditFileSize(label: string, bytes: number) {
  if (bytes > MAX_MASK_EDIT_FILE_BYTES) throw new Error(`${label}超过限制`)
}

export async function getApiErrorMessage(response: Response) {
  try {
    const payload = await response.clone().json() as unknown
    if (payload && typeof payload === 'object') {
      const record = payload as Record<string, unknown>
      const error = record.error
      if (error && typeof error === 'object' && typeof (error as Record<string, unknown>).message === 'string') {
        return (error as Record<string, unknown>).message as string
      }
      if (typeof record.detail === 'string') return record.detail
      if (typeof record.message === 'string') return record.message
    }
  } catch {
    // 非 JSON 响应继续使用状态码。
  }
  return `HTTP ${response.status}`
}

export function pickActualParams(source: unknown): Partial<TaskParams> {
  if (!source || typeof source !== 'object') return {}
  const record = source as Record<string, unknown>
  const params: Partial<TaskParams> = {}
  if (typeof record.size === 'string') params.size = record.size
  if (record.quality === 'auto' || record.quality === 'low' || record.quality === 'medium' || record.quality === 'high' || record.quality === 'xhigh' || record.quality === 'max') params.quality = record.quality
  if (record.output_format === 'png' || record.output_format === 'jpeg' || record.output_format === 'webp') params.output_format = record.output_format
  if (typeof record.output_compression === 'number') params.output_compression = record.output_compression
  if (record.moderation === 'auto' || record.moderation === 'low') params.moderation = record.moderation
  if (typeof record.n === 'number') params.n = record.n
  if (record.partial_images === 0 || record.partial_images === 1 || record.partial_images === 2 || record.partial_images === 3) params.partial_images = record.partial_images
  return params
}

export function mergeActualParams(...sources: Array<Partial<TaskParams> | undefined>) {
  const merged = Object.assign({}, ...sources.filter((source) => source && Object.keys(source).length))
  return Object.keys(merged).length ? merged : undefined
}

export function normalizeImageApiPayload(value: unknown): ImageApiResponse {
  if (!value || typeof value !== 'object') throw new Error('接口返回了无法识别的数据')
  const data = (value as Record<string, unknown>).data
  if (!Array.isArray(data)) throw new Error('接口未返回图片数据')
  return { ...(value as ImageApiResponse), data }
}
