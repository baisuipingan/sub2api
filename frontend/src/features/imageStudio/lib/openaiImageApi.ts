import { libT } from './i18nMessage'
import type { ImageResponseItem, TaskParams } from '../types'
import { imageDataUrlToPngBlob, maskDataUrlToPngBlob, dataUrlToBlob } from './canvasImage'
import { ApiTimeoutError, createApiError, redactSensitiveText } from './apiErrors'
import {
  assertImageInputPayloadSize,
  fetchImageUrlAsDataUrl,
  getApiErrorMessage,
  isHttpUrl,
  MIME_MAP,
  normalizeBase64Image,
  normalizeImageApiPayload,
  pickActualParams,
  mergeActualParams,
  type CallApiOptions,
  type ImageGenerateResult,
} from './imageApiShared'
import { MILO_ROUTER_BASE_URL } from './miloRouterConfig'

function buildUrl(path: string) {
  return `${MILO_ROUTER_BASE_URL}/v1/${path}`
}

function appendFormValue(form: FormData, key: string, value: string | number | boolean | null | undefined) {
  if (value === null || value === undefined || value === '' || value === false) return
  form.append(key, String(value))
}

function appendParams(form: FormData, params: TaskParams) {
  appendFormValue(form, 'size', params.size === 'auto' ? undefined : params.size)
  appendFormValue(form, 'quality', params.quality)
  appendFormValue(form, 'output_format', params.output_format)
  appendFormValue(form, 'output_compression', params.output_compression)
  appendFormValue(form, 'moderation', params.moderation)
  appendFormValue(form, 'n', params.n)
}

function appendStreamingParams(form: FormData, params: TaskParams, stream: boolean) {
  if (!stream) return
  appendFormValue(form, 'stream', true)
  appendFormValue(form, 'partial_images', params.partial_images)
}

export function shouldRequestTransparentBackground(params: TaskParams, nativeTransparentOutput: boolean, hasMask: boolean) {
  return !hasMask && nativeTransparentOutput && params.transparent_output
}

function appendBackgroundParam(form: FormData, params: TaskParams, nativeTransparentOutput: boolean, hasMask: boolean) {
  if (shouldRequestTransparentBackground(params, nativeTransparentOutput, hasMask)) appendFormValue(form, 'background', 'transparent')
}

function buildJsonBody(modelId: string, prompt: string, params: TaskParams, nativeTransparentOutput: boolean, stream: boolean) {
  return {
    model: modelId,
    prompt,
    ...(params.size !== 'auto' ? { size: params.size } : {}),
    quality: params.quality,
    output_format: params.output_format,
    ...(params.output_compression != null ? { output_compression: params.output_compression } : {}),
    moderation: params.moderation,
    n: params.n,
    ...(nativeTransparentOutput && params.transparent_output ? { background: 'transparent' } : {}),
    ...(stream ? { stream: true, partial_images: params.partial_images } : {}),
  }
}

interface ImageStreamEvent {
  type?: string
  b64_json?: string
  partial_image_index?: number
  error?: unknown
  [key: string]: unknown
}

function getStreamError(event: ImageStreamEvent) {
  if (event.type === 'error' && typeof event.message === 'string') return event.message
  if (!event.error) return null
  if (typeof event.error === 'string') return event.error
  if (typeof event.error === 'object' && typeof (event.error as Record<string, unknown>).message === 'string') {
    return (event.error as Record<string, unknown>).message as string
  }
  return '图片流生成失败'
}

async function parseStreamingResponse(response: Response, params: TaskParams, onPartialImage?: CallApiOptions['onPartialImage']): Promise<ImageGenerateResult> {
  if (!response.body) throw new Error(libT('imageStudio.lib.noImageStream'))

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let finalEvent: ImageStreamEvent | null = null

  const processBlock = (block: string) => {
    const data = block.split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trimStart())
      .join('\n')
      .trim()
    if (!data || data === '[DONE]') return

    let event: ImageStreamEvent
    try {
      event = JSON.parse(data) as ImageStreamEvent
    } catch {
      throw new Error(libT('imageStudio.lib.unknownStreamEvent'))
    }
    const streamError = getStreamError(event)
    if (streamError) throw new Error(streamError)

    if ((event.type === 'image_generation.partial_image' || event.type === 'image_edit.partial_image') && typeof event.b64_json === 'string') {
      onPartialImage?.({
        image: normalizeBase64Image(event.b64_json, MIME_MAP[params.output_format] ?? 'image/png'),
        partialIndex: typeof event.partial_image_index === 'number' ? event.partial_image_index : 0,
      })
    }
    if ((event.type === 'image_generation.completed' || event.type === 'image_edit.completed') && typeof event.b64_json === 'string') {
      finalEvent = event
    }
  }

  const drainBlocks = (flush = false) => {
    buffer = buffer.replace(/\r\n/g, '\n')
    let boundary = buffer.indexOf('\n\n')
    while (boundary >= 0) {
      processBlock(buffer.slice(0, boundary))
      buffer = buffer.slice(boundary + 2)
      boundary = buffer.indexOf('\n\n')
    }
    if (flush && buffer.trim()) {
      processBlock(buffer)
      buffer = ''
    }
  }

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    drainBlocks()
  }
  buffer += decoder.decode()
  drainBlocks(true)

  const completed = finalEvent as ImageStreamEvent | null
  if (!completed?.b64_json) throw new Error(libT('imageStudio.lib.streamEndedWithoutImage'))
  const image = normalizeBase64Image(completed.b64_json, MIME_MAP[params.output_format] ?? 'image/png')
  return {
    images: [image],
    actualParams: mergeActualParams(pickActualParams(completed), { n: 1 }),
    actualParamsList: [pickActualParams(completed)],
  }
}

async function parseImageItems(items: ImageResponseItem[], outputFormat: TaskParams['output_format'], signal?: AbortSignal) {
  const images: string[] = []
  const rawImageUrls: string[] = []
  const revisedPrompts: Array<string | undefined> = []

  for (const item of items) {
    if (typeof item.b64_json === 'string' && item.b64_json.trim()) {
      images.push(normalizeBase64Image(item.b64_json.trim(), MIME_MAP[outputFormat] ?? 'image/png'))
      revisedPrompts.push(item.revised_prompt)
      continue
    }

    if (isHttpUrl(item.url)) {
      rawImageUrls.push(item.url)
      images.push(await fetchImageUrlAsDataUrl(item.url, MIME_MAP[outputFormat] ?? 'image/png', signal))
      revisedPrompts.push(item.revised_prompt)
    }
  }

  return { images, rawImageUrls, revisedPrompts }
}

async function parseResponse(response: Response, params: TaskParams, signal?: AbortSignal): Promise<ImageGenerateResult> {
  const payload = normalizeImageApiPayload(await response.json() as unknown)
  const parsed = await parseImageItems(payload.data, params.output_format, signal)
  if (!parsed.images.length) throw new Error(libT('imageStudio.lib.modelReturnedNoImage'))

  return {
    images: parsed.images,
    rawImageUrls: parsed.rawImageUrls.length ? parsed.rawImageUrls : undefined,
    revisedPrompts: parsed.revisedPrompts,
    actualParams: mergeActualParams(
      pickActualParams(payload),
      pickActualParams(payload.data[0]),
      { n: parsed.images.length },
    ),
    actualParamsList: payload.data.map((item) => pickActualParams(item)),
  }
}

export async function callOpenAIImageApi(opts: CallApiOptions): Promise<ImageGenerateResult> {
  const key = opts.settings.miloRouter.apiKey.trim()
  const modelId = opts.modelId ?? opts.settings.selectedModelId
  if (!key) throw createApiError(401)

  const inputBytes = opts.inputImageDataUrls.reduce((total, dataUrl) => total + dataUrl.length, 0)
  assertImageInputPayloadSize(inputBytes)

  const controller = new AbortController()
  const abortRequest = () => controller.abort()
  if (opts.signal?.aborted) abortRequest()
  else opts.signal?.addEventListener('abort', abortRequest, { once: true })
  const signal = controller.signal
  const timeoutId = setTimeout(() => controller.abort(), opts.settings.timeout * 1000)
  const isEdit = opts.inputImageDataUrls.length > 0
  const stream = paramsSupportStreaming(opts, isEdit)

  try {
    let body: BodyInit
    let headers: Record<string, string> = { Authorization: `Bearer ${key}` }

    if (isEdit) {
      const form = new FormData()
      appendFormValue(form, 'model', modelId)
      appendFormValue(form, 'prompt', opts.prompt)
      appendParams(form, opts.params)
      appendBackgroundParam(form, opts.params, opts.nativeTransparentOutput === true, Boolean(opts.maskDataUrl))
      appendStreamingParams(form, opts.params, stream)

      for (const [index, dataUrl] of opts.inputImageDataUrls.entries()) {
        const blob = opts.maskDataUrl && index === 0
          ? await imageDataUrlToPngBlob(dataUrl)
          : await dataUrlToBlob(dataUrl)
        form.append('image[]', blob, `reference-${index + 1}.${blob.type.split('/')[1] || 'png'}`)
      }
      if (opts.maskDataUrl) form.append('mask', await maskDataUrlToPngBlob(opts.maskDataUrl), 'mask.png')
      body = form
    } else {
      headers = { ...headers, 'Content-Type': 'application/json' }
      body = JSON.stringify(buildJsonBody(modelId, opts.prompt, opts.params, opts.nativeTransparentOutput === true, stream))
    }

    const response = await fetch(buildUrl(isEdit ? 'images/edits' : 'images/generations'), {
      method: 'POST',
      headers,
      body,
      cache: 'no-store',
      signal,
    })
    if (!response.ok) throw createApiError(response.status, await getApiErrorMessage(response), key)
    if (stream) return await parseStreamingResponse(response, opts.params, opts.onPartialImage)
    return await parseResponse(response, opts.params, signal)
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw new ApiTimeoutError()
    if (error instanceof Error && error.name.startsWith('Api')) throw error
    throw new Error(redactSensitiveText(error, [key]))
  } finally {
    clearTimeout(timeoutId)
    opts.signal?.removeEventListener('abort', abortRequest)
  }
}

function paramsSupportStreaming(opts: CallApiOptions, isEdit: boolean) {
  if (opts.params.partial_images <= 0) return false
  return isEdit ? opts.streamingEdit === true : opts.streamingGeneration === true
}
