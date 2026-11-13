import type { DesktopAuthorization } from '@/api/desktop'

/** 只允许当前本机授权回调；不能把网站登录变成任意URL重定向入口。 */
export function isDesktopCallback(value: string): boolean {
  if (!/^http:\/\/127\.0\.0\.1:[1-9]\d{0,4}\/callback$/.test(value)) return false
  try {
    const url = new URL(value)
    return Number(url.port) >= 1024 && Number(url.port) <= 65535 && url.href === value
  } catch {
    return false
  }
}

export function parseDesktopAuthorization(query: Record<string, unknown>): DesktopAuthorization | null {
  const required = ['client_id', 'response_type', 'redirect_uri', 'code_challenge', 'code_challenge_method', 'state']
  if (Object.keys(query).length !== required.length || required.some(key => typeof query[key] !== 'string')) return null
  const values = query as Record<string, string>
  if (values.client_id !== 'zcode-desktop' || values.response_type !== 'code' ||
    values.code_challenge_method !== 'S256' || !/^[A-Za-z0-9_-]{43}$/.test(values.code_challenge) ||
    !/^[A-Za-z0-9_-]{22,128}$/.test(values.state) || !isDesktopCallback(values.redirect_uri)) return null
  return { redirect_uri: values.redirect_uri, code_challenge: values.code_challenge, code_challenge_method: 'S256', state: values.state }
}

export function validateDesktopAuthorizationRedirect(value: unknown, request: DesktopAuthorization): value is string {
  if (typeof value !== 'string' || value.length > 2048) return false
  try {
    const expected = new URL(request.redirect_uri)
    const actual = new URL(value)
    const params = [...actual.searchParams.entries()]
    return value.startsWith(`${request.redirect_uri}?`) && actual.origin === expected.origin && actual.pathname === expected.pathname && !actual.username && !actual.password && !actual.hash &&
      params.length === 2 && actual.searchParams.getAll('code').length === 1 && actual.searchParams.getAll('state').length === 1 &&
      /^[A-Za-z0-9_-]{43}$/.test(actual.searchParams.get('code') ?? '') && actual.searchParams.get('state') === request.state
  } catch {
    return false
  }
}

export function desktopAuthorizationDeniedRedirect(request: DesktopAuthorization): string {
  const target = new URL(request.redirect_uri)
  target.searchParams.set('error', 'access_denied')
  target.searchParams.set('state', request.state)
  return target.href
}
