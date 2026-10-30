import { describe, expect, it } from 'vitest'
import { getApiErrorMessageKey, getUserFacingApiErrorMessage } from '../apiErrors'

/**
 * 这些错误文案会直接展示在结果面板和详情弹窗里，
 * 分类错了用户会得到误导性的提示（例如把「超时」说成「密钥无效」）。
 */
describe('getApiErrorMessageKey', () => {
  it('识别鉴权失败', () => {
    expect(getApiErrorMessageKey(new Error('401 unauthorized'))).toBe('imageStudio.errors.auth')
    expect(getApiErrorMessageKey(new Error('API Key 无效'))).toBe('imageStudio.errors.auth')
  })

  it('识别超时', () => {
    expect(getApiErrorMessageKey(new Error('timeout'))).toBe('imageStudio.errors.timeout')
    expect(getApiErrorMessageKey(new Error('连接超时'))).toBe('imageStudio.errors.timeout')
  })

  it('识别限流', () => {
    expect(getApiErrorMessageKey(new Error('429 too many'))).toBe('imageStudio.errors.rateLimit')
  })

  it('识别未返回图片', () => {
    expect(getApiErrorMessageKey(new Error('no image returned'))).toBe('imageStudio.errors.noImage')
  })

  it('识别主动停止', () => {
    expect(getApiErrorMessageKey(new Error('已停止生成'))).toBe('imageStudio.errors.stopped')
  })

  it('未知错误落到通用文案', () => {
    expect(getApiErrorMessageKey(new Error('something odd'))).toBe('imageStudio.errors.generic')
    expect(getApiErrorMessageKey(undefined)).toBe('imageStudio.errors.generic')
  })
})

describe('getUserFacingApiErrorMessage', () => {
  it('不传 t 时返回中文兜底', () => {
    expect(getUserFacingApiErrorMessage(new Error('timeout'))).toBe('连接超时，请稍后重试')
  })

  it('传 t 时返回该语言的文案', () => {
    const t = (key: string) => `translated:${key}`
    expect(getUserFacingApiErrorMessage(new Error('timeout'), t)).toBe(
      'translated:imageStudio.errors.timeout',
    )
  })
})
