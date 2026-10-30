import { libT } from './i18nMessage'
import { assertUsableMaskCoverage, classifyMaskAlpha, type MaskCoverage } from './mask'
import { blobToDataUrl } from './dataUrl'

export async function loadImage(dataUrl: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('图片加载失败'))
    image.src = dataUrl
  })
}

export async function dataUrlToBlob(dataUrl: string, fallbackType = 'image/png'): Promise<Blob> {
  const response = await fetch(dataUrl)
  const blob = await response.blob()
  return blob.type ? blob : new Blob([await blob.arrayBuffer()], { type: fallbackType })
}

export async function imageDataUrlToPngBlob(dataUrl: string): Promise<Blob> {
  const image = await loadImage(dataUrl)
  const canvas = document.createElement('canvas')
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  ctx.drawImage(image, 0, 0)
  return canvasToBlob(canvas, 'image/png')
}

export async function maskDataUrlToPngBlob(maskDataUrl: string): Promise<Blob> {
  const blob = await dataUrlToBlob(maskDataUrl, 'image/png')
  if (blob.type !== 'image/png') {
    return imageDataUrlToPngBlob(maskDataUrl)
  }
  return blob
}

export async function canvasToBlob(canvas: HTMLCanvasElement, type = 'image/png', quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (!blob) reject(new Error('图片导出失败'))
      else resolve(blob)
    }, type, quality)
  })
}

export async function validateMaskMatchesImage(maskDataUrl: string, imageDataUrl: string): Promise<MaskCoverage> {
  const [maskImage, sourceImage] = await Promise.all([loadImage(maskDataUrl), loadImage(imageDataUrl)])
  if (maskImage.naturalWidth !== sourceImage.naturalWidth || maskImage.naturalHeight !== sourceImage.naturalHeight) {
    throw new Error(libT('imageStudio.lib.maskSizeMismatchRedraw'))
  }

  const canvas = document.createElement('canvas')
  canvas.width = maskImage.naturalWidth
  canvas.height = maskImage.naturalHeight
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  ctx.drawImage(maskImage, 0, 0)
  const coverage = classifyMaskAlpha(ctx.getImageData(0, 0, canvas.width, canvas.height))
  assertUsableMaskCoverage(coverage)
  return coverage
}

export async function createMaskPreviewDataUrl(imageDataUrl: string, maskDataUrl: string): Promise<string> {
  const [image, mask] = await Promise.all([loadImage(imageDataUrl), loadImage(maskDataUrl)])
  if (image.naturalWidth !== mask.naturalWidth || image.naturalHeight !== mask.naturalHeight) {
    throw new Error(libT('imageStudio.lib.maskSizeMismatchRedraw'))
  }

  const canvas = document.createElement('canvas')
  canvas.width = image.naturalWidth
  canvas.height = image.naturalHeight
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) throw new Error(libT('imageStudio.lib.canvasUnsupported'))

  ctx.drawImage(image, 0, 0)

  const maskCanvas = document.createElement('canvas')
  maskCanvas.width = mask.naturalWidth
  maskCanvas.height = mask.naturalHeight
  const maskCtx = maskCanvas.getContext('2d', { willReadFrequently: true })
  if (!maskCtx) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  maskCtx.drawImage(mask, 0, 0)
  const maskPixels = maskCtx.getImageData(0, 0, maskCanvas.width, maskCanvas.height)

  const overlay = ctx.createImageData(canvas.width, canvas.height)
  for (let i = 0; i < maskPixels.data.length; i += 4) {
    const editStrength = 255 - maskPixels.data[i + 3]
    overlay.data[i] = 59
    overlay.data[i + 1] = 130
    overlay.data[i + 2] = 246
    overlay.data[i + 3] = Math.round(editStrength * 0.58)
  }

  const overlayCanvas = document.createElement('canvas')
  overlayCanvas.width = canvas.width
  overlayCanvas.height = canvas.height
  const overlayCtx = overlayCanvas.getContext('2d')
  if (!overlayCtx) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  overlayCtx.putImageData(overlay, 0, 0)
  ctx.drawImage(overlayCanvas, 0, 0)
  return canvas.toDataURL('image/png')
}

export function applyMaskToInputPixels(source: Uint8ClampedArray, mask: Uint8ClampedArray) {
  if (source.length !== mask.length) throw new Error(libT('imageStudio.lib.maskSizeMismatchRedraw'))

  const output = new Uint8ClampedArray(source)
  for (let i = 0; i < output.length; i += 4) {
    const preserveStrength = mask[i + 3] / 255
    for (let channel = 0; channel < 3; channel++) {
      output[i + channel] = Math.round(output[i + channel] * preserveStrength + 255 * (1 - preserveStrength))
    }
    output[i + 3] = Math.round(output[i + 3] * preserveStrength)
  }
  return output
}

export function compositeMaskEditPixels(
  source: Uint8ClampedArray,
  generated: Uint8ClampedArray,
  mask: Uint8ClampedArray,
) {
  if (source.length !== generated.length || source.length !== mask.length) {
    throw new Error(libT('imageStudio.lib.editResultSizeMismatch'))
  }

  const output = new Uint8ClampedArray(source.length)
  for (let i = 0; i < output.length; i += 4) {
    const editStrength = 1 - mask[i + 3] / 255
    for (let channel = 0; channel < 4; channel++) {
      output[i + channel] = Math.round(source[i + channel] * (1 - editStrength) + generated[i + channel] * editStrength)
    }
  }
  return output
}

async function loadSameSizePixels(imageDataUrl: string, maskDataUrl: string) {
  const [image, mask] = await Promise.all([loadImage(imageDataUrl), loadImage(maskDataUrl)])
  if (image.naturalWidth !== mask.naturalWidth || image.naturalHeight !== mask.naturalHeight) {
    throw new Error(libT('imageStudio.lib.maskSizeMismatchRedraw'))
  }

  const imageCanvas = document.createElement('canvas')
  imageCanvas.width = image.naturalWidth
  imageCanvas.height = image.naturalHeight
  const imageContext = imageCanvas.getContext('2d', { willReadFrequently: true })
  if (!imageContext) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  imageContext.drawImage(image, 0, 0)

  const maskCanvas = document.createElement('canvas')
  maskCanvas.width = mask.naturalWidth
  maskCanvas.height = mask.naturalHeight
  const maskContext = maskCanvas.getContext('2d', { willReadFrequently: true })
  if (!maskContext) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  maskContext.drawImage(mask, 0, 0)

  return {
    width: image.naturalWidth,
    height: image.naturalHeight,
    imagePixels: imageContext.getImageData(0, 0, imageCanvas.width, imageCanvas.height),
    maskPixels: maskContext.getImageData(0, 0, maskCanvas.width, maskCanvas.height),
  }
}

export async function createMaskedInputImage(imageDataUrl: string, maskDataUrl: string) {
  const loaded = await loadSameSizePixels(imageDataUrl, maskDataUrl)
  loaded.imagePixels.data.set(applyMaskToInputPixels(loaded.imagePixels.data, loaded.maskPixels.data))

  const canvas = document.createElement('canvas')
  canvas.width = loaded.width
  canvas.height = loaded.height
  const context = canvas.getContext('2d')
  if (!context) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  context.putImageData(loaded.imagePixels, 0, 0)
  return blobToDataUrl(await canvasToBlob(canvas, 'image/png'))
}

export async function compositeMaskEditResult(
  sourceDataUrl: string,
  generatedDataUrl: string,
  maskDataUrl: string,
  outputFormat: 'png' | 'jpeg' | 'webp' = 'png',
  outputCompression?: number | null,
) {
  const [loaded, generated] = await Promise.all([
    loadSameSizePixels(sourceDataUrl, maskDataUrl),
    loadImage(generatedDataUrl),
  ])

  const generatedCanvas = document.createElement('canvas')
  generatedCanvas.width = loaded.width
  generatedCanvas.height = loaded.height
  const generatedContext = generatedCanvas.getContext('2d', { willReadFrequently: true })
  if (!generatedContext) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  generatedContext.imageSmoothingEnabled = true
  generatedContext.imageSmoothingQuality = 'high'
  generatedContext.drawImage(generated, 0, 0, generatedCanvas.width, generatedCanvas.height)
  const generatedPixels = generatedContext.getImageData(0, 0, generatedCanvas.width, generatedCanvas.height)
  loaded.imagePixels.data.set(compositeMaskEditPixels(loaded.imagePixels.data, generatedPixels.data, loaded.maskPixels.data))

  const output = document.createElement('canvas')
  output.width = loaded.width
  output.height = loaded.height
  const outputContext = output.getContext('2d')
  if (!outputContext) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  outputContext.putImageData(loaded.imagePixels, 0, 0)
  const mime = outputFormat === 'jpeg' ? 'image/jpeg' : outputFormat === 'webp' ? 'image/webp' : 'image/png'
  return blobToDataUrl(await canvasToBlob(output, mime, outputCompression == null ? undefined : outputCompression / 100))
}
