import { describe, expect, it } from 'vitest'
import { clampLightboxTransform, FIT_TRANSFORM, fittedImage, maximumLightboxScale, pinchLightbox, zoomLightboxAt } from '../lightboxTransform'

const geometry = { viewport: { width: 1000, height: 600 }, image: { width: 2000, height: 1000 } }

describe('lightbox image transforms', () => {
  it('fits the complete image inside the viewport without stretching or enlarging small images', () => {
    expect(fittedImage(geometry)).toEqual({ width: 1000, height: 500, ratio: 0.5 })
    expect(fittedImage({ ...geometry, image: { width: 200, height: 100 } })).toEqual({ width: 200, height: 100, ratio: 1 })
  })

  it('keeps the image point under the zoom anchor stationary', () => {
    const point = { x: 120, y: -40 }
    const zoomed = zoomLightboxAt(FIT_TRANSFORM, point, 2, geometry)
    expect(zoomed).toEqual({ scale: 2, x: -120, y: 40 })
    expect((point.x - zoomed.x) / zoomed.scale).toBe(point.x)
    expect((point.y - zoomed.y) / zoomed.scale).toBe(point.y)
  })

  it('limits dragging to image bounds and centers an axis that does not fill the viewport', () => {
    expect(clampLightboxTransform({ scale: 2, x: 9999, y: -9999 }, geometry)).toEqual({ scale: 2, x: 500, y: -200 })
    const portrait = { ...geometry, image: { width: 1000, height: 2000 } }
    expect(clampLightboxTransform({ scale: 2, x: 400, y: -9999 }, portrait)).toEqual({ scale: 2, x: 0, y: -300 })
  })

  it('combines pinch distance and moving centroid without changing the anchor image point', () => {
    const transform = { scale: 2, x: 10, y: -20 }
    const pinch = pinchLightbox({ transform, startCenter: { x: 100, y: 0 }, nextCenter: { x: 150, y: 30 }, startDistance: 100, nextDistance: 150, geometry })
    expect(pinch).toEqual({ scale: 3, x: 15, y: 0 })
    expect((150 - pinch.x) / pinch.scale).toBe((100 - transform.x) / transform.scale)
  })

  it('resets position when zooming back to fit and refuses to zoom out past fit', () => {
    expect(zoomLightboxAt({ scale: 3, x: 100, y: 150 }, { x: 80, y: 40 }, 0.1, geometry)).toEqual(FIT_TRANSFORM)
  })

  it('permits original-pixel scale even for an image much larger than the viewport', () => {
    const large = { ...geometry, image: { width: 20000, height: 10000 } }
    const fit = fittedImage(large)
    expect(maximumLightboxScale(large)).toBe(20)
    expect(zoomLightboxAt(FIT_TRANSFORM, { x: 0, y: 0 }, 1 / fit.ratio, large).scale * fit.ratio).toBe(1)
  })

  it('bounds excessive wheel scale and handles an incomplete initial geometry', () => {
    expect(zoomLightboxAt(FIT_TRANSFORM, { x: 0, y: 0 }, 1000, geometry).scale).toBe(8)
    const empty = { viewport: { width: 0, height: 0 }, image: { width: 0, height: 0 } }
    expect(fittedImage(empty)).toEqual({ width: 0, height: 0, ratio: 1 })
    expect(pinchLightbox({ transform: FIT_TRANSFORM, startCenter: { x: 0, y: 0 }, nextCenter: { x: 0, y: 0 }, startDistance: 0, nextDistance: 0, geometry: empty })).toEqual(FIT_TRANSFORM)
  })
})
