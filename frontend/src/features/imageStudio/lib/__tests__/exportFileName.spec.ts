import { describe, expect, it } from 'vitest'
import { sanitizeFileNamePart } from '../exportFileName'

describe('image export filenames', () => {
  it('removes filesystem metacharacters and control characters while preserving Unicode names', () => {
    expect(sanitizeFileNamePart('  日落🌄<:\u0000\u001f\u007f\u0085name  .png  '))
      .toBe('日落🌄-name .png')
  })
})
