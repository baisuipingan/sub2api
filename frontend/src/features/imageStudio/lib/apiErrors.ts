export class ApiAuthError extends Error {
  constructor(message = 'API Key 无效或无权限，请检查后重试') {
    super(message)
    this.name = 'ApiAuthError'
  }
}

export class ApiRateLimitError extends Error {
  constructor(message = '请求过于频繁，请稍后重试') {
    super(message)
    this.name = 'ApiRateLimitError'
  }
}

export class ApiTimeoutError extends Error {
  constructor(message = '连接超时，请稍后重试') {
    super(message)
    this.name = 'ApiTimeoutError'
  }
}

export class ApiUpstreamError extends Error {
  constructor(message = 'MiloRouter 服务暂时不可用，请稍后重试') {
    super(message)
    this.name = 'ApiUpstreamError'
  }
}

export class ApiInvalidResponseError extends Error {
  constructor(message = '接口返回了无法识别的数据') {
    super(message)
    this.name = 'ApiInvalidResponseError'
  }
}

export function redactSensitiveText(value: unknown, secrets: string[] = []): string {
  const source = value instanceof Error ? value.message : String(value ?? '')
  const redactedAuthorization = source.replace(/(authorization\s*[:=]\s*bearer\s+)[^\s,;]+/gi, '$1***')
  return secrets
    .map((secret) => secret.trim())
    .filter(Boolean)
    .reduce((text, secret) => text.split(secret).join('***'), redactedAuthorization)
}

export function createApiError(status: number, detail?: unknown, apiKey = ''): Error {
  if (status === 401 || status === 403) return new ApiAuthError()
  if (status === 408) return new ApiTimeoutError()
  if (status === 429) return new ApiRateLimitError()
  if (status >= 500) return new ApiUpstreamError()

  redactSensitiveText(detail, [apiKey])
  return new ApiUpstreamError('请求失败，请稍后重试')
}

export function getUserFacingApiErrorMessage(value: unknown): string {
  const message = value instanceof Error ? value.message : String(value ?? '')
  if (message.includes('已停止生成')) return '已停止生成'
  if (/API Key|\b40[13]\b/i.test(message)) return 'API Key 无效或无权限，请检查后重试'
  if (/超时|timeout|timed out/i.test(message)) return '连接超时，请稍后重试'
  if (/请求过于频繁|rate.?limit|\b429\b/i.test(message)) return '请求过于频繁，请稍后重试'
  if (/未返回图片|没有可用图片|no image/i.test(message)) return '模型未返回图片，请调整提示词后重试'
  if (/暂时不可用|服务异常|service unavailable|\b5\d\d\b/i.test(message)) return 'MiloRouter 服务暂时不可用，请稍后重试'
  return '生成失败，请稍后重试'
}
