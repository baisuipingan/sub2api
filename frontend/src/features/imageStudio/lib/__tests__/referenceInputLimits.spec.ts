import { describe, expect, it, vi } from 'vitest'
vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))
import {
  assertOpenAIMaskBytes,
  assertOpenAIReferenceBytes,
  assertOpenAIReferenceInputs,
  OPENAI_MASK_MAX_BYTES,
  OPENAI_REFERENCE_MAX_BYTES,
  usesOpenAIReferenceLimits,
} from '../referenceInputLimits'

describe('documented OpenAI reference input limits', () => {
  it('uses strict decimal 50 MB per reference image without a pixel cap', () => {
    expect(OPENAI_REFERENCE_MAX_BYTES).toBe(50_000_000)
    expect(() => assertOpenAIReferenceBytes(1_741_565)).not.toThrow()
    expect(() => assertOpenAIReferenceBytes(49_999_999)).not.toThrow()
    expect(() => assertOpenAIReferenceBytes(50_000_000)).toThrow('openaiTooLarge')
    expect(() => assertOpenAIReferenceBytes(0)).toThrow('empty')
  })

  it('validates existing data without decoding or re-encoding image dimensions', () => {
    const original = 'data:image/jpeg;base64,/9j/4Q=='
    expect(() => assertOpenAIReferenceInputs([original])).not.toThrow()
    expect(original).toBe('data:image/jpeg;base64,/9j/4Q==')
  })

  it('requires PNG masks smaller than 4 MB as specified by the edit endpoint', () => {
    expect(OPENAI_MASK_MAX_BYTES).toBe(4_000_000)
    expect(() => assertOpenAIMaskBytes(3_999_999, 'image/png')).not.toThrow()
    expect(() => assertOpenAIMaskBytes(4_000_000, 'image/png')).toThrow('maskTooLarge')
    expect(() => assertOpenAIMaskBytes(10, 'image/jpeg')).toThrow('maskFormat')
    expect(() => assertOpenAIReferenceInputs(['data:image/jpeg;base64,/9j/'], 'data:image/png;base64,iVBORw0KGgo=')).not.toThrow()
    expect(() => assertOpenAIReferenceInputs(['data:image/jpeg;base64,/9j/'], 'data:image/png;base64,/9j/')).toThrow('maskFormat')
  })

  it('keeps Gemini and Grok requirements separate from the OpenAI platform label', () => {
    expect(usesOpenAIReferenceLimits('openai', 'openai')).toBe(true)
    expect(usesOpenAIReferenceLimits('openai', 'unknown')).toBe(true)
    expect(usesOpenAIReferenceLimits('openai', 'google')).toBe(false)
    expect(usesOpenAIReferenceLimits('openai', 'xai')).toBe(false)
    expect(usesOpenAIReferenceLimits('gemini', 'google')).toBe(false)
  })
})
