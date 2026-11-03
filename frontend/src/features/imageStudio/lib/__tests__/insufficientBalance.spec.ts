import { describe, expect, it } from 'vitest'

import {
  ApiAuthError,
  ApiInsufficientBalanceError,
  createApiError,
  getApiErrorMessageKey
} from '../apiErrors'

/**
 * 回归测试：余额不足必须说清楚。
 *
 * 线上真实故障：用户余额 0，/v1/models 返回
 *   403 {"code":"INSUFFICIENT_BALANCE","message":"Insufficient account balance"}
 * 旧实现把 403 一律当成「API Key 无效或无权限」，
 * 用户以为是 Key 的问题，根本想不到要充值。
 *
 * 403 至少承载两种语义，必须靠 reason 区分：
 *   INSUFFICIENT_BALANCE → 去充值
 *   其它 403             → 去换 Key
 */

describe('createApiError 区分 403 的不同语义', () => {
  it('INSUFFICIENT_BALANCE 应产生余额不足错误，而不是鉴权错误', () => {
    const error = createApiError(403, {
      code: 'INSUFFICIENT_BALANCE',
      message: 'Insufficient account balance'
    })

    expect(error).toBeInstanceOf(ApiInsufficientBalanceError)
    expect(error).not.toBeInstanceOf(ApiAuthError)
  })

  it('reason 字段（而非 code）也能识别', () => {
    const error = createApiError(403, {
      reason: 'INSUFFICIENT_BALANCE',
      message: 'Insufficient account balance'
    })
    expect(error).toBeInstanceOf(ApiInsufficientBalanceError)
  })

  it('字符串形式的错误体也能识别', () => {
    const error = createApiError(403, 'INSUFFICIENT_BALANCE: Insufficient account balance')
    expect(error).toBeInstanceOf(ApiInsufficientBalanceError)
  })

  it('嵌套 error.code 形式也能识别', () => {
    const error = createApiError(403, {
      error: { code: 'INSUFFICIENT_BALANCE', message: 'Insufficient account balance' }
    })
    expect(error).toBeInstanceOf(ApiInsufficientBalanceError)
  })

  it('普通 403 仍然是鉴权错误（不能把真鉴权失败误报成余额）', () => {
    const error = createApiError(403, { code: 'FORBIDDEN', message: 'Admin access required' })
    expect(error).toBeInstanceOf(ApiAuthError)
    expect(error).not.toBeInstanceOf(ApiInsufficientBalanceError)
  })

  it('401 仍然是鉴权错误', () => {
    expect(createApiError(401, { message: 'unauthorized' })).toBeInstanceOf(ApiAuthError)
  })
})

describe('getApiErrorMessageKey 给出可照做的文案', () => {
  it('余额不足映射到专属文案，而不是笼统的鉴权失败', () => {
    expect(getApiErrorMessageKey('INSUFFICIENT_BALANCE: Insufficient account balance')).toBe(
      'imageStudio.errors.insufficientBalance'
    )
  })

  it('"insufficient balance" 大小写不同也能识别', () => {
    expect(getApiErrorMessageKey('insufficient account balance')).toBe(
      'imageStudio.errors.insufficientBalance'
    )
  })

  it('真正的鉴权错误仍走鉴权文案', () => {
    expect(getApiErrorMessageKey('API Key 无效')).toBe('imageStudio.errors.auth')
  })
})
