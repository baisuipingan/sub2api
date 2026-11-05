import { libT } from './i18nMessage'
import type { AppSettings, TaskParams } from '../types'
import type { ImageModelCapabilities } from './modelRegistry'
import { ApiInvalidResponseError, ApiTimeoutError, createApiError, redactSensitiveText } from './apiErrors'
import { getApiErrorMessage, normalizeBase64Image, type CallApiOptions, type ImageGenerateResult } from './imageApiShared'
import { MILO_ROUTER_BASE_URL } from './miloRouterConfig'

const ASPECT_RATIOS = ['1:1', '1:4', '4:1', '1:8', '8:1', '2:3', '3:2', '3:4', '4:3', '4:5', '5:4', '9:16', '16:9', '21:9'] as const

export interface GeminiImageConfig {
  aspectRatio?: typeof ASPECT_RATIOS[number]
  imageSize?: '512' | '1K' | '2K' | '4K'
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

export function mapGeminiImageConfig(
  size: string,
  resolution?: TaskParams['resolution'],
  requestedAspectRatio?: string,
  capabilities?: ImageModelCapabilities,
): GeminiImageConfig | undefined {
  const match = size.trim().match(/^(\d+)x(\d+)$/i)
  const width = match ? Number(match[1]) : 0
  const height = match ? Number(match[2]) : 0
  const aspectRatios = ASPECT_RATIOS.filter((ratio) => !capabilities || capabilities.aspectRatios.includes(ratio))
  const explicitAspectRatio = aspectRatios.find((ratio) => ratio === requestedAspectRatio)
  const aspectRatio = requestedAspectRatio !== undefined
    ? explicitAspectRatio
    : width && height && aspectRatios.length
      ? aspectRatios.map((ratio) => {
        const [ratioWidth, ratioHeight] = ratio.split(':').map(Number)
        return { ratio, delta: Math.abs(Math.log((width / height) / (ratioWidth / ratioHeight))) }
      }).sort((a, b) => a.delta - b.delta)[0].ratio
      : undefined
  const pixels = width * height
  const inferredImageSize = pixels
    ? pixels <= 1_572_864 ? '1K' : pixels <= 4_194_304 ? '2K' : '4K'
    : undefined
  const supportsImageSize = !capabilities || capabilities.resolutions.some((tier) => tier === '0.5K' || tier === '2K' || tier === '4K')
  const selectedResolution = resolution === undefined ? inferredImageSize : resolution
  const allowedResolution = selectedResolution && selectedResolution !== 'auto'
    && (!capabilities || capabilities.resolutions.includes(selectedResolution))
  const imageSize = supportsImageSize && allowedResolution
    ? selectedResolution === '0.5K'
      ? capabilities?.resolutions.includes('0.5K') ? '512' : undefined
      : selectedResolution
    : undefined
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

export function buildGeminiImageRequest(
  prompt: string,
  params: TaskParams,
  inputImageDataUrls: string[] = [],
  capabilities?: ImageModelCapabilities,
): GeminiGenerateContentRequest {
  const imageConfig = mapGeminiImageConfig(params.size, params.resolution, params.aspect_ratio, capabilities)
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
      if (!part || typeof part !== 'object' || (part as { thought?: unknown }).thought === true) continue
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
      body: JSON.stringify(buildGeminiImageRequest(opts.prompt, opts.params, opts.inputImageDataUrls, opts.modelCapabilities)),
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
