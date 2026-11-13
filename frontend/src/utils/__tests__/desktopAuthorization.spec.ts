import { describe, expect, it } from 'vitest'
import { isDesktopCallback, parseDesktopAuthorization, validateDesktopAuthorizationRedirect } from '../desktopAuthorization'

const query = () => ({ client_id: 'zcode-desktop', response_type: 'code', redirect_uri: 'http://127.0.0.1:43210/callback', code_challenge: 'a'.repeat(43), code_challenge_method: 'S256', state: 'b'.repeat(43) })

describe('desktop browser authorization boundary', () => {
  it('accepts the exact local client request without exposing query-only fields in the API payload', () => {
    const request = query()
    const { client_id: _client, response_type: _response, ...body } = request
    expect(parseDesktopAuthorization(request)).toEqual(body)
  })
  it.each([
    'https://127.0.0.1:43210/callback', 'http://localhost:43210/callback', 'http://[::1]:43210/callback',
    'http://127.0.0.1:80/callback', 'http://127.0.0.1:0/callback', 'http://127.0.0.1:65536/callback',
    'http://127.0.0.1:04321/callback', 'http://127.0.0.1:43210/callback?', 'http://127.0.0.1:43210/callback#',
    'http://127.0.0.1:43210/%63allback', 'http://user@127.0.0.1:43210/callback', 'http://127.0.0.1.attacker.test:43210/callback',
  ])('rejects a noncanonical callback: %s', value => {
    expect(isDesktopCallback(value)).toBe(false)
    expect(parseDesktopAuthorization({ ...query(), redirect_uri: value })).toBeNull()
  })
  it.each([
    { client_id: 'another-client' }, { response_type: 'token' }, { state: ['a', 'b'] },
    { code_challenge_method: 'plain' }, { code_challenge: 'short' }, { state: '' }, { extra: 'unexpected' },
  ])('rejects malformed, duplicate or unrecognized authorization parameters: %j', override => {
    expect(parseDesktopAuthorization({ ...query(), ...override })).toBeNull()
  })
  it('accepts only a successful redirect bound to this exact authorization', () => {
    const request = parseDesktopAuthorization(query())!
    const result = `${request.redirect_uri}?code=${'c'.repeat(43)}&state=${request.state}`
    expect(validateDesktopAuthorizationRedirect(result, request)).toBe(true)
    for (const altered of [
      result.replace(':43210', ':43211'), result.replace('/callback?', '/other?'),
      result.replace(request.state, 'd'.repeat(43)), `${result}&code=duplicate`, `${result}&extra=1`, `${result}#fragment`,
      `http://127.0.0.1:43210/callback?error=access_denied&state=${request.state}`,
    ]) expect(validateDesktopAuthorizationRedirect(altered, request)).toBe(false)
  })
})
