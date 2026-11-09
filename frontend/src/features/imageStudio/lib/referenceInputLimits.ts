import type { ImageApiKind, ImageModelProvider } from '../types'
import { detectBase64ImageMime, getDataUrlDecodedByteSize } from './imageApiShared'
import { libT } from './i18nMessage'

// OpenAI Images edit API: input images must be less than 50 MB; the explicit
// mask field is PNG and less than 4 MB. These are decimal bytes, not MiB.
// https://developers.openai.com/api/reference/resources/images/methods/edit
export const OPENAI_REFERENCE_MAX_BYTES = 50_000_000
export const OPENAI_MASK_MAX_BYTES = 4_000_000

/** Do not apply OpenAI's documented limits to native or compatible Gemini/Grok
 * model families merely because the account platform is labelled OpenAI. */
export function usesOpenAIReferenceLimits(apiKind?: ImageApiKind, provider?: ImageModelProvider) {
  return apiKind !== 'gemini' && provider !== 'google' && provider !== 'xai'
}

export function assertOpenAIReferenceBytes(bytes: number) {
  if (!Number.isSafeInteger(bytes) || bytes <= 0) throw new Error(libT('imageStudio.galleryInputErrors.empty'))
  if (bytes >= OPENAI_REFERENCE_MAX_BYTES) throw new Error(libT('imageStudio.galleryInputErrors.openaiTooLarge'))
}

export function assertOpenAIMaskBytes(bytes: number, mime: string) {
  if (mime !== 'image/png') throw new Error(libT('imageStudio.galleryInputErrors.maskFormat'))
  if (!Number.isSafeInteger(bytes) || bytes <= 0) throw new Error(libT('imageStudio.galleryInputErrors.empty'))
  if (bytes >= OPENAI_MASK_MAX_BYTES) throw new Error(libT('imageStudio.galleryInputErrors.maskTooLarge'))
}

/** Validation never changes input bytes, dimensions or the image's local ID.
 * The edit API does not document a pixel or longest-edge cap for reference
 * images; output 1K/2K/4K choices do not impose one on the inputs. */
export function assertOpenAIReferenceInputs(inputs: readonly string[], maskDataUrl?: string) {
  for (const input of inputs) assertOpenAIReferenceBytes(getDataUrlDecodedByteSize(input))
  if (maskDataUrl) {
    const encoded = maskDataUrl.match(/^data:[^,]+;base64,([\s\S]*)$/i)?.[1]
    const mime = encoded ? detectBase64ImageMime(encoded) : null
    assertOpenAIMaskBytes(getDataUrlDecodedByteSize(maskDataUrl), mime ?? '')
  }
}
