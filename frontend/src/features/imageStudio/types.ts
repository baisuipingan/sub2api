export type MiloRouterConnectionStatus = 'idle' | 'checking' | 'connected' | 'error'

export interface MiloRouterBillingInfo {
  balance?: number
  effectiveRateMultiplier?: number
  observedAt?: string
}

export interface MiloRouterConfig {
  baseUrl: string
  apiKey: string
  rememberApiKey: boolean
  connectionStatus: MiloRouterConnectionStatus
  billing?: MiloRouterBillingInfo
  error?: string
}

export interface AppSettings {
  miloRouter: MiloRouterConfig
  selectedModelId: string
  connectionOnboardingSeen: boolean
  timeout: number
  clearInputAfterSubmit: boolean
  persistInputOnRestart: boolean
  taskCompletionNotification: boolean
  enterSubmit: boolean
}

export type ImageResolutionTier = 'auto' | '0.5K' | '1K' | '2K' | '4K'
export type ImageModelProvider = 'openai' | 'google' | 'xai' | 'unknown'

export interface TaskParams {
  aspect_ratio?: string
  complexity?: 'auto' | 'low' | 'medium' | 'high'
  resolution: ImageResolutionTier
  size: string
  quality: 'auto' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'
  output_format: 'png' | 'jpeg' | 'webp'
  output_compression: number | null
  moderation: 'auto' | 'low'
  n: number
  transparent_output: boolean
  partial_images: 0 | 1 | 2 | 3
}

export const DEFAULT_PARAMS: TaskParams = {
  resolution: '2K',
  size: 'auto',
  quality: 'auto',
  output_format: 'png',
  output_compression: null,
  moderation: 'auto',
  n: 1,
  transparent_output: false,
  partial_images: 0,
}

export interface InputImage {
  id: string
  dataUrl: string
}

export type ImageEditMode = 'region' | 'erase' | 'background' | 'full'

export interface MaskDraft {
  targetImageId: string
  maskDataUrl: string
  updatedAt: number
  mode?: Extract<ImageEditMode, 'region' | 'erase'>
}

export type TaskStatus = 'running' | 'done' | 'error'
export type ImageApiKind = 'openai' | 'gemini'

export interface TaskRecord {
  id: string
  prompt: string
  params: TaskParams
  apiKind?: ImageApiKind
  modelId?: string
  modelName?: string
  apiModel?: string
  actualParams?: Partial<TaskParams>
  actualParamsByImage?: Record<string, Partial<TaskParams>>
  revisedPromptByImage?: Record<string, string>
  transparentOutput?: boolean
  transparentPrompt?: string
  transparentOriginalImages?: string[]
  inputImageIds: string[]
  editMode?: Extract<ImageEditMode, 'region' | 'erase'>
  maskTargetImageId?: string | null
  maskImageId?: string | null
  outputImages: string[]
  outputErrors?: Array<{ requestIndex: number; error: string }>
  status: TaskStatus
  error: string | null
  createdAt: number
  finishedAt: number | null
  elapsed: number | null
}

export interface StoredImage {
  id: string
  dataUrl: string
  createdAt?: number
  source?: 'upload' | 'generated' | 'mask'
  width?: number
  height?: number
}

export interface StoredImageThumbnail {
  id: string
  thumbnailDataUrl: string
  width?: number
  height?: number
  thumbnailVersion?: number
}

export interface ImageResponseItem {
  b64_json?: string
  url?: string
  revised_prompt?: string
  size?: string
  quality?: string
  output_format?: string
  output_compression?: number
  moderation?: string
}

export interface ImageApiResponse {
  data: ImageResponseItem[]
  size?: string
  quality?: string
  output_format?: string
  output_compression?: number
  moderation?: string
  n?: number
}

export interface ExportData {
  version: number
  exportedAt: string
  backupPart?: {
    id: string
    index: number
    total: number
  }
  settings?: AppSettings
  tasks?: TaskRecord[]
  imageFiles?: Record<string, {
    path: string
    createdAt?: number
    source?: StoredImage['source']
    width?: number
    height?: number
  }>
  thumbnailFiles?: Record<string, {
    path: string
    width?: number
    height?: number
    thumbnailVersion?: number
  }>
}
