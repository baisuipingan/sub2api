import { describe, expect, it } from 'vitest'
import {
  assertUsableMaskCoverage,
  classifyMaskAlpha,
  createMaskConstrainedPrompt,
  orderInputImagesForMask,
  validateMaskTarget,
} from '../mask'
import type { InputImage } from '../../types'

/**
 * 遮罩是「局部重绘」的正确性基石：
 * - 判定错了会把整张图当成遮罩区域（背景被改掉）
 * - 空遮罩没拦住会让上游白跑一次并扣费
 */

function alphaData(alphas: number[]): Pick<ImageData, 'data'> {
  const data = new Uint8ClampedArray(alphas.length * 4)
  alphas.forEach((alpha, index) => {
    data[index * 4 + 3] = alpha
  })
  return { data } as Pick<ImageData, 'data'>
}

function inputImage(id: string): InputImage {
  return { id, name: id, dataUrl: `data:image/png;base64,${id}` } as InputImage
}

describe('classifyMaskAlpha', () => {
  it('全部不透明 → empty（用户没涂）', () => {
    expect(classifyMaskAlpha(alphaData([255, 255, 255]))).toBe('empty')
  })

  it('全部透明 → full（等于整图重绘）', () => {
    expect(classifyMaskAlpha(alphaData([0, 0, 0]))).toBe('full')
  })

  it('部分透明 → partial（正常的局部编辑）', () => {
    expect(classifyMaskAlpha(alphaData([255, 0, 255]))).toBe('partial')
  })

  it('半透明边缘也算已编辑', () => {
    expect(classifyMaskAlpha(alphaData([255, 128]))).toBe('partial')
  })
})

describe('assertUsableMaskCoverage', () => {
  it('空遮罩必须报错，避免白跑一次生成', () => {
    expect(() => assertUsableMaskCoverage('empty')).toThrow()
  })

  it('partial 与 full 放行', () => {
    expect(() => assertUsableMaskCoverage('partial')).not.toThrow()
    expect(() => assertUsableMaskCoverage('full')).not.toThrow()
  })
})

describe('validateMaskTarget', () => {
  it('找到目标图时原样返回', () => {
    const images = [inputImage('a'), inputImage('b')]
    expect(validateMaskTarget(images, 'b').id).toBe('b')
  })

  it('目标不存在时抛错', () => {
    expect(() => validateMaskTarget([inputImage('a')], 'missing')).toThrow()
  })
})

describe('orderInputImagesForMask', () => {
  it('把遮罩主图提到第一位，其余保持原顺序', () => {
    const images = [inputImage('a'), inputImage('b'), inputImage('c')]
    const ordered = orderInputImagesForMask(images, 'c')
    expect(ordered.map((item) => item.id)).toEqual(['c', 'a', 'b'])
  })

  it('目标不存在时抛错', () => {
    expect(() => orderInputImagesForMask([inputImage('a')], 'x')).toThrow()
  })
})

describe('createMaskConstrainedPrompt', () => {
  it('保留用户提示词并附加遮罩约束', () => {
    const prompt = createMaskConstrainedPrompt('把猫换成狗')
    expect(prompt).toContain('把猫换成狗')
    expect(prompt).toContain('遮罩区域')
  })

  it('用户提示词前后空格会被裁掉', () => {
    expect(createMaskConstrainedPrompt('  改颜色  ')).toContain('编辑要求：改颜色')
  })
})
