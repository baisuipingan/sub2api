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

/**
 * 余额/额度不足。
 *
 * 必须与「API Key 无效」区分开：两者都是 403，但处理方式完全不同——
 * 前者去充值，后者去换 Key。混成一句「API Key 无效」会让用户白折腾。
 */
export class ApiInsufficientBalanceError extends Error {
  constructor(message = '账户余额不足，请先充值') {
    super(message)
    this.name = 'ApiInsufficientBalanceError'
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

/**
 * 从后端错误体里取 reason。
 *
 * 后端可能返回 {code,reason,message} 或 {error:{code,message}} 两种形状，
 * 这里都兼容；取不到就返回空串，交由状态码兜底判断。
 */
function extractErrorReason(detail: unknown): string {
  if (!detail) return ''
  let payload: unknown = detail
  if (typeof detail === 'string') {
    try {
      payload = JSON.parse(detail)
    } catch {
      return ''
    }
  }
  if (typeof payload !== 'object' || payload === null) return ''
  const record = payload as Record<string, unknown>
  if (typeof record.reason === 'string') return record.reason
  if (typeof record.code === 'string') return record.code
  const nested = record.error
  if (typeof nested === 'object' && nested !== null) {
    const inner = nested as Record<string, unknown>
    if (typeof inner.code === 'string') return inner.code
  }
  return ''
}

export function createApiError(status: number, detail?: unknown, apiKey = ''): Error {
  // 先看后端给的 reason：403 里至少有两种完全不同的语义，不能一律当鉴权失败。
  // detail 可能是对象，也可能是 "REASON: message" 形式的字符串，两种都要认。
  const reason = extractErrorReason(detail)
  if (reason === 'INSUFFICIENT_BALANCE' || /INSUFFICIENT_BALANCE|insufficient\s+(account\s+)?balance/i.test(String(detail ?? ''))) {
    return new ApiInsufficientBalanceError()
  }
  if (reason === 'USAGE_LIMIT_EXCEEDED') return new ApiRateLimitError('已超出用量上限')

  if (status === 401 || status === 403) return new ApiAuthError()
  if (status === 408) return new ApiTimeoutError()
  if (status === 429) return new ApiRateLimitError()
  if (status >= 500) return new ApiUpstreamError()

  redactSensitiveText(detail, [apiKey])
  return new ApiUpstreamError('请求失败，请稍后重试')
}

/**
 * 把底层错误翻译成用户能看懂的一句话。
 *
 * 传入 t 时返回对应语言的文案；不传则返回中文兜底（store 里的 toast 没有
 * i18n 上下文，仍走兜底）。所有分支都用 i18n key，避免把文案写死在 lib 里。
 */
export function getApiErrorMessageKey(value: unknown): string {
  const message = value instanceof Error ? value.message : String(value ?? '')
  if (message.includes('已停止生成')) return 'imageStudio.errors.stopped'
  // 余额不足必须排在鉴权判断之前：它同样是 403，但处理方式完全不同
  if (/INSUFFICIENT_BALANCE|insufficient\s+(account\s+)?balance/i.test(message)) {
    return 'imageStudio.errors.insufficientBalance'
  }
  if (/API Key|\b40[13]\b/i.test(message)) return 'imageStudio.errors.auth'
  if (/超时|timeout|timed out/i.test(message)) return 'imageStudio.errors.timeout'
  if (/请求过于频繁|rate.?limit|\b429\b/i.test(message)) return 'imageStudio.errors.rateLimit'
  if (/未返回图片|没有可用图片|no image/i.test(message)) return 'imageStudio.errors.noImage'
  if (/暂时不可用|服务异常|service unavailable|\b5\d\d\b/i.test(message)) return 'imageStudio.errors.unavailable'
  return 'imageStudio.errors.generic'
}

const FALLBACK_MESSAGES: Record<string, string> = {
  'imageStudio.errors.stopped': '已停止生成',
  'imageStudio.errors.auth': 'API Key 无效或无权限，请检查后重试',
  'imageStudio.errors.timeout': '连接超时，请稍后重试',
  'imageStudio.errors.rateLimit': '请求过于频繁，请稍后重试',
  'imageStudio.errors.noImage': '模型未返回图片，请调整提示词后重试',
  'imageStudio.errors.unavailable': 'MiloRouter 服务暂时不可用，请稍后重试',
  'imageStudio.errors.generic': '生成失败，请稍后重试',
}

export function getUserFacingApiErrorMessage(
  value: unknown,
  t?: (key: string) => string,
): string {
  const key = getApiErrorMessageKey(value)
  return t ? t(key) : (FALLBACK_MESSAGES[key] ?? FALLBACK_MESSAGES['imageStudio.errors.generic'])
}
