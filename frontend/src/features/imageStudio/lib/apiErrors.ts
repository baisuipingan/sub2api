interface ApiErrorDetails {
  status?: number
  code?: string
  detail?: string
}

export class ApiRequestError extends Error {
  readonly status?: number
  readonly code?: string
  readonly detail?: string

  constructor(message = '请求失败，请稍后重试', details: ApiErrorDetails = {}) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = details.status
    this.code = details.code
    this.detail = details.detail
  }
}

export class ApiAuthError extends ApiRequestError {
  constructor(message = 'API Key 无效或无权限，请检查后重试', details?: ApiErrorDetails) {
    super(message, details)
    this.name = 'ApiAuthError'
  }
}

export class ApiRateLimitError extends ApiRequestError {
  constructor(message = '请求过于频繁，请稍后重试', details?: ApiErrorDetails) {
    super(message, details)
    this.name = 'ApiRateLimitError'
  }
}

export class ApiTimeoutError extends ApiRequestError {
  constructor(message = '连接超时，请稍后重试', details?: ApiErrorDetails) {
    super(message, details)
    this.name = 'ApiTimeoutError'
  }
}

export class ApiUpstreamError extends ApiRequestError {
  constructor(message = 'MiloRouter 服务暂时不可用，请稍后重试', details?: ApiErrorDetails) {
    super(message, details)
    this.name = 'ApiUpstreamError'
  }
}

export class ApiInsufficientBalanceError extends ApiRequestError {
  constructor(message = '账户余额不足，请先充值', details?: ApiErrorDetails) {
    super(message, details)
    this.name = 'ApiInsufficientBalanceError'
  }
}

export class ApiInvalidResponseError extends ApiRequestError {
  constructor(message = '接口返回了无法识别的数据', details?: ApiErrorDetails) {
    super(message, details)
    this.name = 'ApiInvalidResponseError'
  }
}

export function redactSensitiveText(value: unknown, secrets: string[] = []): string {
  const source = value instanceof Error ? value.message : String(value ?? '')
  let redacted = source
    .replace(/((?:["']?authorization["']?)\s*[:=]\s*)(?:"[^"]*"|'[^']*'|(?:bearer|basic)\s+[^\s,;}]+|[^\s,;}]+)/gi, '$1***')
    .replace(/(["']?(?:cookie|set-cookie)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\r\n]+)/gi, '$1***')
    .replace(/\b(bearer|basic)\s+[a-z0-9._~+\/-]+=*/gi, '$1 ***')
    .replace(/(["']?(?:api[_ -]?key|x-api-key|x-goog-api-key|access[_-]?token|refresh[_-]?token|id[_-]?token|password|passwd|client[_-]?secret|secret|token|cookie|set-cookie)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}\]&]+)/gi, '$1***')
    .replace(/\b(?:sk|rk)-[a-z0-9_-]{8,}\b/gi, '***')
    .replace(/\beyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+\b/gi, '***')
    .replace(/(https?:\/\/)[^\s/:@]+:[^\s/@]+@/gi, '$1***@')
    .replace(/([?&](?:key|api_key|token|access_token|signature|sig)=)[^&\s"'#]+/gi, '$1***')
  for (const secret of secrets.map((item) => item.trim()).filter(Boolean)) {
    redacted = redacted.split(secret).join('***')
  }
  return redacted
}

function safeMessage(value: string, secrets: string[] = []) {
  // A proxy's HTML error page is not an actionable provider message.
  if (/<\/?(?:!doctype|html|head|body|script|style|title|div|span|pre|h[1-6]|p|img|iframe|table|br)\b/i.test(value)) return ''
  return redactSensitiveText(value, secrets)
    .replace(/https?:\/\/[^\s<>"']+/gi, '[URL redacted]')
    .replace(/data:[^\s"']+|[A-Za-z0-9+/=_-]{160,}/gi, '[image or token redacted]')
    .replace(/\p{Cc}/gu, (character) => '\n\r\t'.includes(character) ? character : '').trim()
}

function errorDetail(value: unknown, depth = 0): { code?: string; message?: string } {
  if (depth > 3 || value == null) return {}
  if (value instanceof Error) return { message: value.message }
  if (typeof value === 'string') {
    const message = value.trim()
    if (!message) return {}
    try {
      const parsed = JSON.parse(message) as unknown
      if (parsed && typeof parsed === 'object') return errorDetail(parsed, depth + 1)
    } catch { /* Plain-text provider messages are supported too. */ }
    const prefixed = message.match(/^([A-Z][A-Z0-9_]+|[a-z]+(?:_[a-z]+)+):\s*([\s\S]+)$/)
    return prefixed ? { code: prefixed[1], message: prefixed[2] } : { message }
  }
  if (typeof value !== 'object' || Array.isArray(value)) return {}
  const record = value as Record<string, unknown>
  const nested = errorDetail(record.error, depth + 1)
  const code = [record.reason, record.code, record.status].find((item) => typeof item === 'string')
  const message = [record.message, record.detail, record.description].find((item) => typeof item === 'string')
  return {
    code: nested.code ?? (typeof code === 'string' ? code : undefined),
    message: nested.message ?? (typeof message === 'string' ? message : undefined),
  }
}

export function createApiError(status: number, detail?: unknown, apiKey = ''): ApiRequestError {
  const parsed = errorDetail(detail)
  const message = safeMessage(parsed.message ?? '', [apiKey])
  const code = parsed.code && /^[a-z0-9_.-]{1,100}$/i.test(parsed.code)
    ? redactSensitiveText(parsed.code, [apiKey])
    : undefined
  const details = { status, code, detail: message || undefined }
  const reason = `${parsed.code ?? ''} ${parsed.message ?? ''}`
  if (/INSUFFICIENT_BALANCE|insufficient\s+(account\s+)?balance/i.test(reason)) {
    return new ApiInsufficientBalanceError(undefined, details)
  }
  if (/USAGE_LIMIT_EXCEEDED/.test(reason)) return new ApiRateLimitError('已超出用量上限', details)
  if (status === 401 || status === 403) return new ApiAuthError(undefined, details)
  if (status === 408) return new ApiTimeoutError(undefined, details)
  if (status === 429) return new ApiRateLimitError(undefined, details)
  if (status >= 500) return new ApiUpstreamError(undefined, details)
  return new ApiRequestError(undefined, details)
}

export function getApiErrorMessageKey(value: unknown): string {
  const detail = value instanceof ApiRequestError ? `${value.code ?? ''} ${value.detail ?? ''}` : ''
  const message = `${detail} ${value instanceof Error ? value.message : String(value ?? '')}`
  if (/MODERATION_BLOCKED|CONTENT_POLICY_VIOLATION|SAFETY_VIOLATION|safety system|safety review|审核拒绝/i.test(message)) return 'imageStudio.errors.moderation'
  if (/UPSTREAM_NO_IMAGE|UPSTREAM_INVALID_RESPONSE|INVALID_IMAGE_RESULT|no image returned|invalid image result/i.test(message)) return 'imageStudio.errors.noImage'
  if (value instanceof ApiInsufficientBalanceError) return 'imageStudio.errors.insufficientBalance'
  if (value instanceof ApiAuthError) return 'imageStudio.errors.auth'
  if (value instanceof ApiRateLimitError) return 'imageStudio.errors.rateLimit'
  if (value instanceof ApiTimeoutError) return 'imageStudio.errors.timeout'
  if (value instanceof ApiUpstreamError) return 'imageStudio.errors.unavailable'
  if (value instanceof ApiRequestError) return 'imageStudio.errors.generic'
  if (message.includes('已停止生成')) return 'imageStudio.errors.stopped'
  if (/INSUFFICIENT_BALANCE|insufficient\s+(account\s+)?balance|余额不足/i.test(message)) return 'imageStudio.errors.insufficientBalance'
  if (/API Key|\b40[13]\b/i.test(message)) return 'imageStudio.errors.auth'
  if (/超时|timeout|timed out/i.test(message)) return 'imageStudio.errors.timeout'
  if (/请求过于频繁|rate.?limit|\b429\b|USAGE_LIMIT_EXCEEDED/i.test(message)) return 'imageStudio.errors.rateLimit'
  if (/未返回图片|没有可用图片|no image/i.test(message)) return 'imageStudio.errors.noImage'
  if (/暂时不可用|服务异常|service unavailable|\b5\d\d\b/i.test(message)) return 'imageStudio.errors.unavailable'
  return 'imageStudio.errors.generic'
}

const FALLBACK_MESSAGES: Record<string, string> = {
  'imageStudio.errors.stopped': '已停止生成',
  'imageStudio.errors.insufficientBalance': '账户余额不足，请先充值',
  'imageStudio.errors.auth': 'API Key 无效或无权限，请检查后重试',
  'imageStudio.errors.timeout': '连接超时，请稍后重试',
  'imageStudio.errors.rateLimit': '请求过于频繁，请稍后重试',
  'imageStudio.errors.moderation': '上游安全审核拒绝了本次生成，请调整提示词或参考图片',
  'imageStudio.errors.noImage': '模型未返回图片，请调整提示词后重试',
  'imageStudio.errors.unavailable': 'MiloRouter 服务暂时不可用，请稍后重试',
  'imageStudio.errors.generic': '生成失败，请稍后重试',
}

function isSummaryOnly(message: string) {
  return Object.values(FALLBACK_MESSAGES).includes(message)
    || /^(?:timeout|timed out|连接超时|已停止生成|请求失败，请稍后重试|接口返回了无法识别的数据|HTTP\s+\d{3})$/i.test(message)
}

/** Safe details are persisted in TaskRecord.error and must survive rendering again. */
export function getUserFacingApiErrorMessage(value: unknown, t?: (key: string) => string): string {
  const key = getApiErrorMessageKey(value)
  const summary = t ? t(key) : (FALLBACK_MESSAGES[key] ?? FALLBACK_MESSAGES['imageStudio.errors.generic'])
  if (value instanceof ApiRequestError) {
    const context = [value.status ? `HTTP ${value.status}` : '', value.code ? `[${value.code}]` : ''].filter(Boolean).join(' ')
    const detail = safeMessage(value.detail ?? value.message)
    return [summary, [context, detail && !isSummaryOnly(detail) && detail !== summary ? detail : ''].filter(Boolean).join(': ')].filter(Boolean).join('\n')
  }
  const message = safeMessage(value instanceof Error ? value.message : typeof value === 'string' ? value : '')
  if (!message || isSummaryOnly(message)) return summary
  // Multi-line text has already been formatted for persistent task history.
  return typeof value === 'string' && message.includes('\n') ? message : `${summary}\n${message}`
}

/** A short readable reason stays visible; technical details can be expanded. */
export function getImageErrorPresentation(value: unknown, t?: (key: string) => string) {
  const message = getUserFacingApiErrorMessage(value, t)
  const [summary = '', ...lines] = message.split('\n')
  return { summary, details: lines.join('\n').trim() }
}
