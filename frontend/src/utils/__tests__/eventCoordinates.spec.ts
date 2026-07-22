import { describe, expect, it } from 'vitest'
import { gcj02ToWgs84, wgs84ToGcj02 } from '../eventCoordinates'

describe('event coordinate conversion', () => {
  it('keeps coordinates outside mainland China unchanged', () => {
    expect(wgs84ToGcj02(35.6762, 139.6503)).toEqual([35.6762, 139.6503])
    expect(gcj02ToWgs84(35.6762, 139.6503)).toEqual([35.6762, 139.6503])
  })

  it('round-trips a Shanghai venue within marker-level precision', () => {
    const source: [number, number] = [31.2304, 121.4737]
    const converted = wgs84ToGcj02(...source)
    const restored = gcj02ToWgs84(...converted)

    expect(converted).not.toEqual(source)
    expect(restored[0]).toBeCloseTo(source[0], 4)
    expect(restored[1]).toBeCloseTo(source[1], 4)
  })
})
