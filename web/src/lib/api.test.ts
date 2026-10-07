import { describe, it, expect, vi, afterEach, assert } from 'vitest'
import { api, ApiError, onUnauthorized, errorMessage, isApiError, caughtErrorMessage } from './api'
import { decodeUser } from '../types/domain'
import { ContractError } from '../types/validation'
import { userFixture } from '../test-fixtures.fixture'

function mockFetch(status: number, body: unknown) {
  const response = new Response(status === 204 ? null : JSON.stringify(body), { status })
  const fn = vi.fn<typeof fetch>().mockResolvedValue(response)
  vi.stubGlobal('fetch', fn)
  return fn
}

async function apiFailure(operation: Promise<unknown>): Promise<ApiError> {
  try { await operation } catch (error: unknown) {
    assert(error instanceof ApiError, 'Expected a structured API failure')
    return error
  }
  throw new Error('Expected the request to fail')
}

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('api', () => {
  it('sends same-origin credentials and JSON, never an Authorization header', async () => {
    const fetch = mockFetch(200, { ok: true })
    await api('/api/x', { method: 'POST', json: { a: 1 } })
    const call = fetch.mock.calls[0]
    assert(call)
    const [, init] = call
    assert(init)
    expect(init.credentials).toBe('same-origin')
    const headers = new Headers(init.headers)
    expect(headers.get('Content-Type')).toBe('application/json')
    expect(headers.has('Authorization')).toBe(false)
    expect(init.body).toBe('{"a":1}')
  })

  it('returns null for 204', async () => {
    mockFetch(204, null)
    expect(await api('/api/x', { method: 'DELETE' })).toBeNull()
  })

  it('throws ApiError with a translated message', async () => {
    mockFetch(409, { error: { code: 'CONFLICT', message: 'username is already taken' } })
    const err = await apiFailure(api('/api/auth/register', { method: 'POST', json: {} }))
    expect(err.status).toBe(409)
    expect(err.code).toBe('CONFLICT')
    expect(err.message).toBe('Dieser Benutzername ist bereits vergeben')
  })

  it('notifies listeners on 401, except for a failed login', async () => {
    const listener = vi.fn()
    const off = onUnauthorized(listener)
    try {
      mockFetch(401, { error: { code: 'UNAUTHORIZED', message: 'authentication required' } })
      await apiFailure(api('/api/channels'))
      expect(listener).toHaveBeenCalledTimes(1)
      mockFetch(401, { error: { code: 'INVALID_CREDENTIALS', message: 'invalid username or password' } })
      await apiFailure(api('/api/auth/login', { method: 'POST', json: {} }))
      expect(listener).toHaveBeenCalledTimes(1)
    } finally { off() }
  })

  it('maps network failures to a readable error', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch')))
    const err = await apiFailure(api('/api/x'))
    expect(err.code).toBe('NETWORK')
  })

  it.each([null, [], { error: null }, { error: { code: 17, message: false } }])(
    'handles malformed server error bodies without a secondary exception: %j', async body => {
      mockFetch(500, body)
      const error = await apiFailure(api('/api/x'))
      expect(error.status).toBe(500)
      expect(error.code).toBe('INTERNAL_ERROR')
      expect(error.message.length).toBeGreaterThan(0)
    },
  )

  it('preserves cancellation identity and supplies the AbortSignal to fetch', async () => {
    const controller = new AbortController()
    const aborted = new DOMException('Canceled', 'AbortError')
    const fetch = vi.fn<typeof globalThis.fetch>().mockRejectedValue(aborted)
    vi.stubGlobal('fetch', fetch)
    await expect(api('/api/x', { signal: controller.signal })).rejects.toBe(aborted)
    expect(fetch).toHaveBeenCalledWith('/api/x', expect.objectContaining({ signal: controller.signal }))
  })

  it('requires the explicit decoder to accept successful JSON before returning typed data', async () => {
    const user = userFixture({ username: 'alice' })
    mockFetch(200, user)
    expect(await api('/api/user', { decode: decodeUser })).toEqual(user)
    mockFetch(200, { id: 'not-a-uuid', content: 'SYNTHETIC_PRIVATE_PAYLOAD' })
    const logs = [vi.spyOn(console, 'log'), vi.spyOn(console, 'warn'), vi.spyOn(console, 'error')]
    await expect(api('/api/user', { decode: decodeUser })).rejects.toBeInstanceOf(ContractError)
    for (const log of logs) expect(log).not.toHaveBeenCalled()
  })

  it('rejects malformed successful JSON through the explicit decoder', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValue(new Response('{broken', { status: 200 })))
    await expect(api('/api/user', { decode: decodeUser })).rejects.toBeInstanceOf(ContractError)
  })
})

describe('errorMessage', () => {
  it('translates validation patterns', () => {
    expect(errorMessage('INVALID_INPUT', 'password must be at least 10 characters')).toBe('Das Passwort muss mindestens 10 Zeichen haben')
    expect(errorMessage('INVALID_INPUT', 'content must be at most 4000 characters')).toBe('Nachricht: höchstens 4000 Zeichen')
    expect(errorMessage('RATE_LIMITED', 'too many requests')).toMatch(/Zu viele Versuche/)
  })
})

describe('API boundary edge cases', () => {
  it('preserves FormData transport without setting a manual boundary header', async () => {
    const fetch = mockFetch(200, { ok: true })
    const form = new FormData()
    form.set('file', new Blob(['synthetic']), 'synthetic.txt')
    await api('/api/upload', { method: 'POST', form })
    expect(fetch).toHaveBeenCalledWith('/api/upload', expect.objectContaining({ body: form, headers: {}, credentials: 'same-origin' }))
  })
  it('runs the decoder for empty successful responses and returns malformed JSON as null without a decoder', async () => {
    const decode = vi.fn((value: unknown) => value === null ? 'empty' : 'unexpected')
    mockFetch(204, null)
    expect(await api('/api/x', { decode })).toBe('empty')
    expect(decode).toHaveBeenCalledWith(null)
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValue(new Response('broken', { status: 200 })))
    expect(await api('/api/x')).toBeNull()
  })
  it('bounds error fields and formats all actionable validation messages', async () => {
    mockFetch(500, { error: { code: 'x'.repeat(65), message: 'synthetic' } })
    expect((await apiFailure(api('/api/x'))).code).toBe('INTERNAL_ERROR')
    expect(errorMessage('INVALID_INPUT', 'unknown must be at most 3 characters')).toContain('unknown')
    expect(errorMessage('INVALID_INPUT', 'password must be at most 100 characters')).toMatch(/Passwort/)
    expect(errorMessage('INVALID_INPUT', 'username must be valid')).toMatch(/Benutzername/)
    expect(errorMessage('INVALID_INPUT', 'file exceeds the 10 MB limit')).toContain('10')
    expect(errorMessage('INVALID_INPUT', 'synthetic input')).toBe('synthetic input')
    expect(errorMessage(null, null)).toBeTruthy()
    expect(errorMessage('unknown', 'x'.repeat(1500))).toHaveLength(1024)
  })
})


describe('caught error display', () => {
  it('distinguishes structured API failures and bounds arbitrary caught messages', () => {
    expect(isApiError(new ApiError(401, 'UNAUTHORIZED', 'synthetic'))).toBe(true)
    expect(isApiError(new Error('ordinary'))).toBe(false)
    expect(caughtErrorMessage(new Error('ordinary'), 'fallback')).toBe('ordinary')
    expect(caughtErrorMessage({ message: 'x'.repeat(1200) }, 'fallback')).toHaveLength(1024)
    for (const value of [null, undefined, [], 'thrown text', { message: '' }, { message: 1 }]) expect(caughtErrorMessage(value, 'fallback')).toBe('fallback')
  })
})
