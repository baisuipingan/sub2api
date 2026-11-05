import { describe, expect, it } from 'vitest'
import {
  ApiAuthError, ApiInsufficientBalanceError, ApiRateLimitError, ApiRequestError, ApiUpstreamError,
  createApiError, getApiErrorMessageKey, getUserFacingApiErrorMessage, redactSensitiveText,
} from '../apiErrors'

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

describe('safe provider error details', () => {
  it('preserves structured request status, code, and an actionable detail', () => {
    const error = createApiError(400, { error: { code: 'unsupported_size', message: 'The current model does not support 4K. Choose 2K.' } })

    expect(error).toBeInstanceOf(ApiRequestError)
    expect(error).toMatchObject({ status: 400, code: 'unsupported_size', detail: 'The current model does not support 4K. Choose 2K.' })
    const message = getUserFacingApiErrorMessage(error)
    expect(message).toContain('HTTP 400 [unsupported_size]')
    expect(message).toContain('Choose 2K.')
  })

  it('recognizes code-prefixed adapter messages without dropping their details', () => {
    const error = createApiError(400, 'INVALID_ARGUMENT: imageSize 4K is unsupported for this model')
    expect(error.code).toBe('INVALID_ARGUMENT')
    expect(getUserFacingApiErrorMessage(error)).toContain('imageSize 4K is unsupported')
  })

  it('keeps a safe 5xx upstream summary as well as the provider message', () => {
    const error = createApiError(502, { code: 'request_failed', message: 'The image provider is restarting. Retry in 30 seconds.' })
    expect(error).toBeInstanceOf(ApiUpstreamError)
    expect(getUserFacingApiErrorMessage(error)).toContain('MiloRouter 服务暂时不可用')
    expect(getUserFacingApiErrorMessage(error)).toContain('HTTP 502 [request_failed]')
    expect(getUserFacingApiErrorMessage(error)).toContain('Retry in 30 seconds.')
  })

  it.each([
    [401, 'UNAUTHORIZED', ApiAuthError, 'imageStudio.errors.auth'],
    [403, 'INSUFFICIENT_BALANCE', ApiInsufficientBalanceError, 'imageStudio.errors.insufficientBalance'],
    [429, 'rate_limit_exceeded', ApiRateLimitError, 'imageStudio.errors.rateLimit'],
    [403, 'USAGE_LIMIT_EXCEEDED', ApiRateLimitError, 'imageStudio.errors.rateLimit'],
  ] as const)('retains the %s category while carrying safe provider detail', (status, code, ErrorType, messageKey) => {
    const error = createApiError(status, { code, message: 'Provider explanation for this request' })
    expect(error).toBeInstanceOf(ErrorType)
    expect(getApiErrorMessageKey(error)).toBe(messageKey)
    expect(getUserFacingApiErrorMessage(error, (key) => `translated:${key}`)).toContain(`translated:${messageKey}`)
    expect(getUserFacingApiErrorMessage(error)).toContain('Provider explanation')
  })

  it('redacts known keys, authorization, passwords, tokens, and URL credentials before storing', () => {
    const error = createApiError(400, {
      message: 'Try 2K. supplied-key Authorization: Bearer dummy-secret-value api_key="other-key" password: "secret password" refresh_token=refresh-value https://user:pass@example.test/image?token=query-secret',
    }, 'supplied-key')
    const message = getUserFacingApiErrorMessage(error)
    expect(message).toContain('Try 2K.')
    for (const secret of ['supplied-key', 'dummy-secret-value', 'other-key', 'secret password', 'refresh-value', 'user:pass', 'query-secret']) {
      expect(error.detail).not.toContain(secret)
      expect(message).not.toContain(secret)
    }
    expect(message).toContain('***')
  })

  it('does not display a proxy HTML page or stringify arbitrary response objects', () => {
    const error = createApiError(502, '<html><body>Proxy error page</body></html>')
    expect(error.detail).toBeUndefined()
    expect(getUserFacingApiErrorMessage(error)).not.toContain('Proxy error page')
    expect(getUserFacingApiErrorMessage(error)).toContain('HTTP 502')
    expect(getUserFacingApiErrorMessage(createApiError(400, { password: 'hidden-value' }))).not.toContain('hidden-value')
  })

  it('does not classify a detailed request error as auth merely because it mentions an API key', () => {
    const error = createApiError(400, { message: 'This API key group does not allow 4K; choose 2K.' })
    expect(getApiErrorMessageKey(error)).toBe('imageStudio.errors.generic')
    expect(getUserFacingApiErrorMessage(error)).toContain('choose 2K.')
  })

  it('keeps persisted messages intact when the result panel and details format them again', () => {
    const message = getUserFacingApiErrorMessage(createApiError(400, { message: 'Choose 2K for the selected model.' }))
    expect(getUserFacingApiErrorMessage(message, (key) => `translated:${key}`)).toBe(message)
    expect(getUserFacingApiErrorMessage(getUserFacingApiErrorMessage(message))).toBe(message)
  })

  it('sanitizes credentials again when an old stored message is displayed', () => {
    const message = getUserFacingApiErrorMessage('Request failed\nHTTP 400: Use 2K. access_token=old-secret')
    expect(message).toContain('Use 2K.')
    expect(message).not.toContain('old-secret')
    expect(redactSensitiveText('Authorization="Basic dGVzdDp0ZXN0"')).not.toContain('dGVzdDp0ZXN0')
    expect(redactSensitiveText('Retry later. Cookie: session=old-cookie; auth=private-cookie')).toBe('Retry later. Cookie: ***')
    expect(redactSensitiveText('https://example.test/?key=google-private&sig=signed-private')).toBe('https://example.test/?key=***&sig=***')
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
