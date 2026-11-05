import { libT } from './i18nMessage'
import { ApiInvalidResponseError, ApiTimeoutError, createApiError, redactSensitiveText } from './apiErrors'
import {
  assertImageInputPayloadSize,
  fetchImageUrlAsDataUrl,
  getApiErrorMessage,
  isHttpUrl,
  mergeActualParams,
  normalizeBase64Image,
  normalizeImageApiPayload,
  pickActualParams,
  type CallApiOptions,
  type ImageGenerateResult,
} from './imageApiShared'
import { MILO_ROUTER_BASE_URL } from './miloRouterConfig'

// Source snapshot: output/docs/image-providers/grok-images-api-reference.md.
const ASPECT_RATIOS = ['1:1', '3:4', '4:3', '9:16', '16:9', '2:3', '3:2', '9:19.5', '19.5:9', '9:20', '20:9', '1:2', '2:1', '21:9', '5:2', 'auto'] as const
const QUALITIES = ['auto', 'low', 'medium'] as const

function imageAspectRatio(opts: CallApiOptions) {
  const explicit = opts.params.aspect_ratio?.trim()
  if (explicit) {
    if (!ASPECT_RATIOS.includes(explicit as typeof ASPECT_RATIOS[number])) throw new Error('Unsupported Grok image aspect ratio')
    return explicit
  }
  const match = opts.params.size.trim().match(/^(\d+)x(\d+)$/i)
  if (!match || Number(match[1]) <= 0 || Number(match[2]) <= 0) return undefined
  const ratio = Number(match[1]) / Number(match[2])
  return ASPECT_RATIOS.filter((value) => value !== 'auto')
    .map((value) => {
      const [width, height] = value.split(':').map(Number)
      return { value, delta: Math.abs(Math.log(ratio / (width / height))) }
    })
    .sort((a, b) => a.delta - b.delta)[0].value
}

export function buildGrokImageRequest(opts: CallApiOptions) {
  if (opts.maskDataUrl) throw new Error(libT('imageStudio.lib.modelNoMaskEditing'))
  const model = opts.modelId ?? opts.settings.selectedModelId
  if (!model.trim()) throw new Error('Image model is required')
  const resolution = opts.params.resolution
  if (resolution !== 'auto' && resolution !== '1K' && resolution !== '2K') throw new Error('Grok images support 1K or 2K resolution')
  const count = opts.params.n
  const maxCount = Math.min(10, opts.modelCapabilities?.imageCount.max ?? 10)
  if (!Number.isInteger(count) || count < 1 || count > maxCount) throw new Error(`Grok image count must be between 1 and ${maxCount}`)
  const maxReferences = Math.min(5, opts.modelCapabilities?.referenceImages ?? 3)
  if (opts.inputImageDataUrls.length > maxReferences) throw new Error(`This model supports at most ${maxReferences} reference images`)
  const images = opts.inputImageDataUrls.map((source) => {
    const url = source.trim()
    if (!isHttpUrl(url) && !/^data:image\/(?:png|jpe?g|webp);base64,[a-z0-9+/=\s]+$/i.test(url)) {
      throw new Error('Grok reference images must be JPEG, PNG, or WebP images')
    }
    return { url, type: 'image_url' as const }
  })
  assertImageInputPayloadSize(images.reduce((total, image) => total + image.url.length, 0))
  const aspectRatio = imageAspectRatio(opts)
  const quality = opts.params.quality
  const prompt = opts.prompt.replace(/\[image (\d+)\]/g, (mention, value: string) => {
    const index = Number(value) - 1
    return index >= 0 && index < images.length ? `<IMAGE_${index}>` : mention
  })
  return {
    model,
    prompt,
    n: count,
    response_format: 'b64_json' as const,
    // xAI documents omitted resolution as 1k. Send that default explicitly so
    // an Images-compatible gateway can attribute its routing/billing tier.
    resolution: resolution === 'auto' ? '1k' : resolution.toLowerCase(),
    ...(aspectRatio ? { aspect_ratio: aspectRatio } : {}),
    ...(QUALITIES.includes(quality as typeof QUALITIES[number]) && opts.modelCapabilities?.qualities.includes(quality) ? { quality } : {}),
    ...(images.length === 1 ? { image: images[0] } : images.length > 1 ? { images } : {}),
  }
}

async function parseGrokImageResponse(response: Response, signal: AbortSignal): Promise<ImageGenerateResult> {
  const payload = normalizeImageApiPayload(await response.json() as unknown)
  const images: string[] = []
  const rawImageUrls: string[] = []
  const actualParamsList = []
  const revisedPrompts = []
  for (const item of payload.data) {
    const mimeType = (item as unknown as Record<string, unknown>).mime_type
    const mime = typeof mimeType === 'string' ? mimeType : 'image/jpeg'
    if (typeof item.b64_json === 'string' && item.b64_json.trim()) {
      images.push(normalizeBase64Image(item.b64_json, mime))
    } else if (isHttpUrl(item.url)) {
      rawImageUrls.push(item.url)
      images.push(await fetchImageUrlAsDataUrl(item.url, mime, signal))
    } else {
      continue
    }
    actualParamsList.push(pickActualParams(item))
    revisedPrompts.push(item.revised_prompt)
  }
  if (!images.length) throw new ApiInvalidResponseError(libT('imageStudio.lib.modelReturnedNoImage'))
  return {
    images,
    ...(rawImageUrls.length ? { rawImageUrls } : {}),
    actualParams: mergeActualParams(pickActualParams(payload), { n: images.length }),
    actualParamsList,
    revisedPrompts,
  }
}

export async function callGrokImageApi(opts: CallApiOptions): Promise<ImageGenerateResult> {
  const key = opts.settings.miloRouter.apiKey.trim()
  if (!key) throw createApiError(401)
  const body = buildGrokImageRequest(opts)
  const controller = new AbortController()
  const abort = () => controller.abort()
  if (opts.signal?.aborted) abort()
  else opts.signal?.addEventListener('abort', abort, { once: true })
  const timeout = setTimeout(abort, opts.settings.timeout * 1000)
  try {
    const endpoint = opts.inputImageDataUrls.length ? 'images/edits' : 'images/generations'
    const response = await fetch(`${MILO_ROUTER_BASE_URL}/v1/${endpoint}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      cache: 'no-store',
      signal: controller.signal,
    })
    if (!response.ok) throw createApiError(response.status, await getApiErrorMessage(response), key)
    return await parseGrokImageResponse(response, controller.signal)
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw new ApiTimeoutError()
    if (error instanceof Error && error.name.startsWith('Api')) throw error
    throw new Error(redactSensitiveText(error, [key]))
  } finally {
    clearTimeout(timeout)
    opts.signal?.removeEventListener('abort', abort)
  }
}
