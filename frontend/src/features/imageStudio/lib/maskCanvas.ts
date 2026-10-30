import { libT } from './i18nMessage'
import { canvasToBlob } from './canvasImage'
import { blobToDataUrl } from './dataUrl'

export interface MaskCanvasSize {
  width: number
  height: number
}

export function fillWhiteMask(canvas: HTMLCanvasElement) {
  const context = canvas.getContext('2d', { willReadFrequently: true })
  if (!context) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  context.globalCompositeOperation = 'source-over'
  context.clearRect(0, 0, canvas.width, canvas.height)
  context.fillStyle = '#fff'
  context.fillRect(0, 0, canvas.width, canvas.height)
}

export function drawMaskImageToCanvas(maskImage: HTMLImageElement, maskCanvas: HTMLCanvasElement) {
  const maskAspect = maskImage.naturalWidth / maskImage.naturalHeight
  const canvasAspect = maskCanvas.width / maskCanvas.height
  if (Math.abs(maskAspect - canvasAspect) > 0.001) throw new Error(libT('imageStudio.lib.maskSizeMismatch'))

  const context = maskCanvas.getContext('2d', { willReadFrequently: true })
  if (!context) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  context.clearRect(0, 0, maskCanvas.width, maskCanvas.height)
  context.imageSmoothingEnabled = true
  context.imageSmoothingQuality = 'high'
  context.drawImage(maskImage, 0, 0, maskCanvas.width, maskCanvas.height)
}

export function hasEditableMaskPixels(canvas: HTMLCanvasElement) {
  const pixels = canvas.getContext('2d', { willReadFrequently: true })?.getImageData(0, 0, canvas.width, canvas.height).data
  if (!pixels) return false
  for (let index = 3; index < pixels.length; index += 4) {
    if (pixels[index] < 250) return true
  }
  return false
}

export async function createOriginalSizeMaskDataUrl(canvas: HTMLCanvasElement, originalSize: MaskCanvasSize) {
  if (canvas.width === originalSize.width && canvas.height === originalSize.height) {
    return await blobToDataUrl(await canvasToBlob(canvas, 'image/png'))
  }

  const output = document.createElement('canvas')
  output.width = originalSize.width
  output.height = originalSize.height
  const context = output.getContext('2d')
  if (!context) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  context.imageSmoothingEnabled = true
  context.imageSmoothingQuality = 'high'
  context.drawImage(canvas, 0, 0, output.width, output.height)
  return await blobToDataUrl(await canvasToBlob(output, 'image/png'))
}
