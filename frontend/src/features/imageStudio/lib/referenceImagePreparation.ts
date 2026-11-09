import { blobToDataUrl, imageSourceToBlob } from './dataUrl'
import { canvasToBlob, loadImage } from './canvasImage'
import { detectBase64ImageMime, getDataUrlDecodedByteSize } from './imageApiShared'
import { libT } from './i18nMessage'

export const GALLERY_REFERENCE_MAX_PIXELS = 20_000_000
export const GALLERY_REFERENCE_MAX_BYTES = 32 << 20
const PREPARED_MAX_EDGE = 4096

/** Preserve ordinary references byte-for-byte. Only oversized originals need a
 * separate smaller submission copy; original history and draft IDs stay intact. */
export function referencePreparationSize(width: number, height: number, bytes: number, forcedMaxEdge?: number) {
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width <= 0 || height <= 0) throw new Error(libT('imageStudio.galleryInputErrors.decode'))
  if (!forcedMaxEdge && width * height <= GALLERY_REFERENCE_MAX_PIXELS && bytes <= GALLERY_REFERENCE_MAX_BYTES) return null
  const scale = Math.min(1, (forcedMaxEdge ?? PREPARED_MAX_EDGE) / Math.max(width, height), Math.sqrt(GALLERY_REFERENCE_MAX_PIXELS / (width * height)))
  return { width: Math.max(1, Math.floor(width * scale)), height: Math.max(1, Math.floor(height * scale)) }
}

async function resizeImage(image: HTMLImageElement, width: number, height: number, mime: string, preservePNG = false): Promise<string> {
  const canvas = document.createElement('canvas')
  canvas.width = width; canvas.height = height
  try {
    const context = canvas.getContext('2d')
    if (!context) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
    context.drawImage(image, 0, 0, width, height)
    // JPEG has no alpha. Other inputs use PNG first so transparent references
    // and mask coverage remain intact; WebP is a bounded fallback.
    let blob = await canvasToBlob(canvas, mime, 0.92)
    if (!preservePNG && blob.size > GALLERY_REFERENCE_MAX_BYTES) blob = await canvasToBlob(canvas, 'image/webp', 0.92)
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(blob.type) || blob.size > GALLERY_REFERENCE_MAX_BYTES) throw new Error(libT('imageStudio.galleryInputErrors.tooLarge'))
    return await blobToDataUrl(blob)
  } finally { canvas.width = 0; canvas.height = 0 }
}

export interface PreparedStudioReference {
  dataUrl: string
  maskDataUrl?: string
  width: number
  height: number
  changed: boolean
}

export async function prepareStudioReference(dataUrl: string, maskDataUrl?: string, forcedMaxEdge?: number): Promise<PreparedStudioReference> {
  const source = await loadImage(dataUrl)
  let mask: HTMLImageElement | undefined
  try {
    const width = source.naturalWidth; const height = source.naturalHeight
    const bytes = Math.max(getDataUrlDecodedByteSize(dataUrl), maskDataUrl ? getDataUrlDecodedByteSize(maskDataUrl) : 0)
    const size = referencePreparationSize(width, height, bytes, forcedMaxEdge)
    if (!size) return { dataUrl, maskDataUrl, width, height, changed: false }
    if (maskDataUrl) {
      mask = await loadImage(maskDataUrl)
      if (mask.naturalWidth !== width || mask.naturalHeight !== height) throw new Error(libT('imageStudio.lib.maskSizeMismatchRedraw'))
    }
    const encoded = dataUrl.match(/^data:[^,]+;base64,([\s\S]*)$/i)?.[1]
    const actualMIME = encoded ? detectBase64ImageMime(encoded) : (await imageSourceToBlob(dataUrl)).type
    let target = size
    let preparedMask: string | undefined
    if (mask) {
      try { preparedMask = await resizeImage(mask, target.width, target.height, 'image/png', true) }
      catch (error) {
        if (Math.max(target.width, target.height) <= 2560) throw error
        // A noisy alpha mask must remain PNG. A 2560px RGBA canvas fits within
        // 32 MiB even before compression; reduce both images together.
        const smaller = referencePreparationSize(width, height, bytes, 2560)
        if (!smaller) throw error
        target = smaller
        preparedMask = await resizeImage(mask, target.width, target.height, 'image/png', true)
      }
    }
    const prepared = await resizeImage(source, target.width, target.height, actualMIME === 'image/jpeg' ? 'image/jpeg' : 'image/png')
    return { dataUrl: prepared, ...(preparedMask ? { maskDataUrl: preparedMask } : {}), ...target, changed: true }
  } finally { source.src = ''; if (mask) mask.src = '' }
}
