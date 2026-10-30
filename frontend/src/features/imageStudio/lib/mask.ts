import { libT } from './i18nMessage'
import type { InputImage } from '../types'

export type MaskCoverage = 'empty' | 'partial' | 'full'

export function createMaskConstrainedPrompt(prompt: string): string {
  return [
    '严格执行局部遮罩编辑：透明遮罩区域是唯一允许修改的位置。',
    '所有新增、替换或移除的内容都必须位于遮罩区域内；遮罩外的主体、背景、构图、光线、颜色和细节保持不变。',
    '不要移除整张图片的背景，不要把未遮罩区域变成透明。',
    `编辑要求：${prompt.trim()}`,
  ].join('\n')
}

export function validateMaskTarget(inputImages: InputImage[], targetImageId: string): InputImage {
  const target = inputImages.find((img) => img.id === targetImageId)
  if (!target) throw new Error(libT('imageStudio.lib.maskTargetMissing'))
  return target
}

export function orderInputImagesForMask(inputImages: InputImage[], targetImageId: string): InputImage[] {
  const target = validateMaskTarget(inputImages, targetImageId)
  return [target, ...inputImages.filter((img) => img.id !== targetImageId)]
}

export function classifyMaskAlpha(imageData: Pick<ImageData, 'data'>): MaskCoverage {
  let edited = 0
  let fullyTransparent = 0
  const total = imageData.data.length / 4

  for (let i = 3; i < imageData.data.length; i += 4) {
    if (imageData.data[i] < 255) edited++
    if (imageData.data[i] === 0) fullyTransparent++
  }

  if (edited === 0) return 'empty'
  if (fullyTransparent === total) return 'full'
  return 'partial'
}

export function assertUsableMaskCoverage(coverage: MaskCoverage): void {
  if (coverage === 'empty') {
    throw new Error(libT('imageStudio.lib.maskAreaRequired'))
  }
}
