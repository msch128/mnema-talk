import { describe, it, expect, vi, afterEach } from 'vitest'
import { api, ApiError, onUnauthorized, errorMessage } from './api'

function mockFetch(status, body) {
  const fn = vi.fn().mockResolvedValue({
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body)
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

describe('api', () => {
  it('sends same-origin credentials and JSON, never an Authorization header', async () => {
    const fetch = mockFetch(200, { ok: true })
    await api('/api/x', { method: 'POST', json: { a: 1 } })
    const [, init] = fetch.mock.calls[0]
    expect(init.credentials).toBe('same-origin')
    expect(init.headers['Content-Type']).toBe('application/json')
    expect(init.headers.Authorization).toBeUndefined()
    expect(init.body).toBe('{"a":1}')
  })

  it('returns null for 204', async () => {
    mockFetch(204, null)
    expect(await api('/api/x', { method: 'DELETE' })).toBeNull()
  })

  it('throws ApiError with a translated message', async () => {
    mockFetch(409, { error: { code: 'CONFLICT', message: 'username is already taken' } })
    const err = await api('/api/auth/register', { method: 'POST', json: {} }).catch(e => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(409)
    expect(err.code).toBe('CONFLICT')
    expect(err.message).toBe('Dieser Benutzername ist bereits vergeben')
  })

  it('notifies listeners on 401, except for a failed login', async () => {
    const listener = vi.fn()
    const off = onUnauthorized(listener)
    mockFetch(401, { error: { code: 'UNAUTHORIZED', message: 'authentication required' } })
    await api('/api/channels').catch(() => {})
    expect(listener).toHaveBeenCalledTimes(1)
    mockFetch(401, { error: { code: 'INVALID_CREDENTIALS', message: 'invalid username or password' } })
    await api('/api/auth/login', { method: 'POST', json: {} }).catch(() => {})
    expect(listener).toHaveBeenCalledTimes(1)
    off()
  })

  it('maps network failures to a readable error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const err = await api('/api/x').catch(e => e)
    expect(err.code).toBe('NETWORK')
  })
})

describe('errorMessage', () => {
  it('translates validation patterns', () => {
    expect(errorMessage('INVALID_INPUT', 'password must be at least 10 characters')).toBe('Das Passwort muss mindestens 10 Zeichen haben')
    expect(errorMessage('INVALID_INPUT', 'content must be at most 4000 characters')).toBe('Nachricht: höchstens 4000 Zeichen')
    expect(errorMessage('RATE_LIMITED', 'too many requests')).toMatch(/Zu viele Versuche/)
  })
})
