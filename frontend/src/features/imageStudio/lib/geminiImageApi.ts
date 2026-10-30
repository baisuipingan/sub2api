import { libT } from './i18nMessage'
import type { AppSettings, TaskParams } from '../types'
import { ApiInvalidResponseError, ApiTimeoutError, createApiError, redactSensitiveText } from './apiErrors'
import { getApiErrorMessage, normalizeBase64Image, type CallApiOptions, type ImageGenerateResult } from './imageApiShared'
import { MILO_ROUTER_BASE_URL } from './miloRouterConfig'

const ASPECT_RATIOS = ['1:1', '2:3', '3:2', '3:4', '4:3', '4:5', '5:4', '9:16', '16:9', '21:9'] as const

export interface GeminiImageConfig {
  aspectRatio?: typeof ASPECT_RATIOS[number]
  imageSize?: '1K' | '2K' | '4K'
}

export interface GeminiGenerateContentRequest {
  contents: Array<{
    role: 'user'
    parts: Array<{ text: string } | { inlineData: { mimeType: string; data: string } }>
  }>
  generationConfig: {
    responseModalities: ['IMAGE']
    imageConfig?: GeminiImageConfig
  }
}

export function mapGeminiImageConfig(size: string, resolution?: TaskParams['resolution']): GeminiImageConfig | undefined {
  const match = size.trim().match(/^(\d+)x(\d+)$/i)
  const width = match ? Number(match[1]) : 0
  const height = match ? Number(match[2]) : 0
  const aspectRatio = width && height
    ? ASPECT_RATIOS
        .map((ratio) => {
          const [ratioWidth, ratioHeight] = ratio.split(':').map(Number)
          return { ratio, delta: Math.abs(Math.log((width / height) / (ratioWidth / ratioHeight))) }
        })
        .sort((a, b) => a.delta - b.delta)[0].ratio
    : undefined
  const pixels = width * height
  const inferredImageSize = pixels
    ? pixels <= 1_572_864 ? '1K' : pixels <= 4_194_304 ? '2K' : '4K'
    : undefined
  const imageSize = resolution === undefined
    ? inferredImageSize
    : resolution === 'auto' ? undefined : resolution
  return aspectRatio || imageSize ? { ...(aspectRatio ? { aspectRatio } : {}), ...(imageSize ? { imageSize } : {}) } : undefined
}

export function parseGeminiImageDataUrl(dataUrl: string) {
  const match = dataUrl.trim().match(/^data:(image\/[a-z0-9.+-]+);base64,([\s\S]+)$/i)
  if (!match) throw new Error(libT('imageStudio.lib.referenceMustBeBase64'))
  return {
    inlineData: {
      mimeType: match[1].toLowerCase(),
      data: match[2].trim(),
    },
  }
}

export function buildGeminiImageRequest(prompt: string, params: TaskParams, inputImageDataUrls: string[] = []): GeminiGenerateContentRequest {
  const imageConfig = mapGeminiImageConfig(params.size, params.resolution)
  return {
    contents: [{
      role: 'user',
      parts: [
        { text: prompt },
        ...inputImageDataUrls.map(parseGeminiImageDataUrl),
      ],
    }],
    generationConfig: {
      responseModalities: ['IMAGE'],
      ...(imageConfig ? { imageConfig } : {}),
    },
  }
}

export function parseGeminiImageResponse(payload: unknown): ImageGenerateResult {
  if (!payload || typeof payload !== 'object') throw new ApiInvalidResponseError('模型未返回图片，请调整提示词后重试')
  const candidates = (payload as { candidates?: unknown }).candidates
  if (!Array.isArray(candidates)) throw new ApiInvalidResponseError('模型未返回图片，请调整提示词后重试')

  const images: string[] = []
  for (const candidate of candidates) {
    if (!candidate || typeof candidate !== 'object') continue
    const content = (candidate as { content?: unknown }).content
    if (!content || typeof content !== 'object') continue
    const parts = (content as { parts?: unknown }).parts
    if (!Array.isArray(parts)) continue
    for (const part of parts) {
      if (!part || typeof part !== 'object') continue
      const inlineData = (part as { inlineData?: unknown }).inlineData
      if (!inlineData || typeof inlineData !== 'object') continue
      const record = inlineData as Record<string, unknown>
      if (typeof record.data !== 'string' || !record.data.trim()) continue
      if (typeof record.mimeType !== 'string' || !record.mimeType.startsWith('image/')) continue
      images.push(normalizeBase64Image(record.data, record.mimeType))
    }
  }

  if (!images.length) throw new ApiInvalidResponseError('模型未返回图片，请调整提示词后重试')
  return { images, actualParams: { n: images.length }, actualParamsList: images.map(() => ({ n: 1 })) }
}

function getUrl(modelId: string) {
  return `${MILO_ROUTER_BASE_URL}/v1beta/models/${encodeURIComponent(modelId)}:generateContent`
}

export async function callGeminiImageApi(opts: CallApiOptions): Promise<ImageGenerateResult> {
  if (opts.maskDataUrl) throw new Error(libT('imageStudio.lib.modelNoMaskEditing'))
  const key = opts.settings.miloRouter.apiKey.trim()
  const modelId = opts.modelId ?? opts.settings.selectedModelId
  if (!key) throw createApiError(401)

  const controller = new AbortController()
  const abortRequest = () => controller.abort()
  if (opts.signal?.aborted) abortRequest()
  else opts.signal?.addEventListener('abort', abortRequest, { once: true })
  const signal = controller.signal
  const timeoutId = setTimeout(() => controller.abort(), opts.settings.timeout * 1000)
  try {
    const response = await fetch(getUrl(modelId), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${key}`,
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      body: JSON.stringify(buildGeminiImageRequest(opts.prompt, opts.params, opts.inputImageDataUrls)),
      signal,
    })
    if (!response.ok) throw createApiError(response.status, await getApiErrorMessage(response), key)
    return parseGeminiImageResponse(await response.json() as unknown)
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw new ApiTimeoutError()
    if (error instanceof Error && error.name.startsWith('Api')) throw error
    throw new Error(redactSensitiveText(error, [key]))
  } finally {
    clearTimeout(timeoutId)
    opts.signal?.removeEventListener('abort', abortRequest)
  }
}

export function getGeminiRequestSettings(settings: AppSettings) {
  return settings.miloRouter
}
