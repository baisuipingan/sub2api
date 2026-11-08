import { libT } from './i18nMessage'
import { ApiInvalidResponseError, getUserFacingApiErrorMessage } from './apiErrors'
import { callGeminiImageApi } from './geminiImageApi'
import { getImageModel } from './modelRegistry'
import { callOpenAIImageApi } from './openaiImageApi'
import { callGrokImageApi } from './grokImageApi'
import { MAX_OUTPUT_IMAGES } from './paramCompatibility'
import type { CallApiOptions, ImageGenerateResult } from './imageApiShared'

export type { CallApiOptions, CallApiResult, ImageGenerateResult } from './imageApiShared'

const IMAGE_REQUEST_CONCURRENCY = 2

async function callSingleImageApi(opts: CallApiOptions): Promise<ImageGenerateResult> {
  const modelId = opts.modelId ?? opts.settings.selectedModelId
  const apiKind = opts.apiKind ?? getImageModel(modelId)?.api
  if (!modelId || !apiKind) throw new ApiInvalidResponseError('请选择有效的生图模型')

  if (apiKind === 'gemini') return callGeminiImageApi({ ...opts, modelId })
  return callOpenAIImageApi({ ...opts, modelId })
}

export async function callImageApi(opts: CallApiOptions): Promise<ImageGenerateResult> {
  if (opts.apiKind !== 'gemini' && opts.modelProvider === 'xai') return callGrokImageApi(opts)
  const requestCount = Math.min(MAX_OUTPUT_IMAGES, Math.max(1, Math.floor(opts.params.n)))
  if (requestCount === 1) return callSingleImageApi(opts)

  const results: Array<{ requestIndex: number; result: ImageGenerateResult }> = []
  const failures: Array<{ requestIndex: number; error: string; cause: unknown }> = []
  let nextRequestIndex = 0

  const runWorker = async () => {
    while (nextRequestIndex < requestCount) {
      if (opts.signal?.aborted) return
      const requestIndex = nextRequestIndex
      nextRequestIndex += 1
      try {
        const result = await callSingleImageApi({
          ...opts,
          params: { ...opts.params, n: 1 },
          onPartialImage: opts.onPartialImage
            ? (partial) => opts.onPartialImage?.({ ...partial, requestIndex })
            : undefined,
        })
        results.push({ requestIndex, result })
      } catch (error) {
        failures.push({ requestIndex, error: getUserFacingApiErrorMessage(error), cause: error })
      }
    }
  }

  await Promise.all(Array.from({ length: Math.min(IMAGE_REQUEST_CONCURRENCY, requestCount) }, runWorker))
  if (!results.length) throw failures.sort((a, b) => a.requestIndex - b.requestIndex)[0]?.cause ?? new Error(libT('imageStudio.lib.modelReturnedNoImage'))

  results.sort((a, b) => a.requestIndex - b.requestIndex)
  const images = results.map(({ result }) => result.images[0]).filter((image): image is string => Boolean(image))
  const actualParamsList = results.map(({ result }) => result.actualParamsList?.[0] ?? result.actualParams)
  const revisedPrompts = results.map(({ result }) => result.revisedPrompts?.[0])

  return {
    images,
    actualParams: { ...results[0].result.actualParams, n: images.length },
    actualParamsList,
    revisedPrompts,
    ...(failures.length ? {
      failedRequests: failures
        .sort((a, b) => a.requestIndex - b.requestIndex)
        .map(({ requestIndex, error }) => ({ requestIndex, error })),
    } : {}),
  }
}
