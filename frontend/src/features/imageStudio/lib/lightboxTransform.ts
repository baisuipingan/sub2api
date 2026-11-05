export interface LightboxPoint { x: number; y: number }
export interface LightboxSize { width: number; height: number }
export interface LightboxTransform extends LightboxPoint { scale: number }
export interface LightboxGeometry {
  viewport: LightboxSize
  image: LightboxSize
}

export const FIT_TRANSFORM: LightboxTransform = { scale: 1, x: 0, y: 0 }

export function fittedImage(geometry: LightboxGeometry) {
  const { image, viewport } = geometry
  if (image.width <= 0 || image.height <= 0 || viewport.width <= 0 || viewport.height <= 0) {
    return { width: 0, height: 0, ratio: 1 }
  }
  const ratio = Math.min(1, viewport.width / image.width, viewport.height / image.height)
  return { width: image.width * ratio, height: image.height * ratio, ratio }
}

export function maximumLightboxScale(geometry: LightboxGeometry) {
  return Math.max(8, 1 / fittedImage(geometry).ratio)
}

export function clampLightboxTransform(transform: LightboxTransform, geometry: LightboxGeometry): LightboxTransform {
  const fitted = fittedImage(geometry)
  const scale = Math.min(maximumLightboxScale(geometry), Math.max(1, transform.scale))
  const maxX = Math.max(0, (fitted.width * scale - geometry.viewport.width) / 2)
  const maxY = Math.max(0, (fitted.height * scale - geometry.viewport.height) / 2)
  return {
    scale,
    x: Math.min(maxX, Math.max(-maxX, transform.x)),
    y: Math.min(maxY, Math.max(-maxY, transform.y)),
  }
}

/** Points are relative to the viewport center; translation is in screen pixels. */
export function zoomLightboxAt(transform: LightboxTransform, point: LightboxPoint, nextScale: number, geometry: LightboxGeometry) {
  const scale = Math.min(maximumLightboxScale(geometry), Math.max(1, nextScale))
  return clampLightboxTransform({
    scale,
    x: point.x - (point.x - transform.x) * scale / transform.scale,
    y: point.y - (point.y - transform.y) * scale / transform.scale,
  }, geometry)
}

export function pinchLightbox(input: {
  transform: LightboxTransform
  startCenter: LightboxPoint
  nextCenter: LightboxPoint
  startDistance: number
  nextDistance: number
  geometry: LightboxGeometry
}) {
  const scale = Math.min(maximumLightboxScale(input.geometry), Math.max(1,
    input.transform.scale * (input.startDistance > 0 ? input.nextDistance / input.startDistance : 1)))
  return clampLightboxTransform({
    scale,
    x: input.nextCenter.x - (input.startCenter.x - input.transform.x) * scale / input.transform.scale,
    y: input.nextCenter.y - (input.startCenter.y - input.transform.y) * scale / input.transform.scale,
  }, input.geometry)
}
