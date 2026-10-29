import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { classifyMediaFailure, knownGone, resetMediaStatus } from './mediaStatus'

const answer = (status: number) => vi.fn(() => Promise.resolve(new Response('', { status })))

beforeEach(resetMediaStatus)
afterEach(() => vi.unstubAllGlobals())

describe('classifyMediaFailure', () => {
  it.each([404, 410])('calls a %i gone and remembers it', async status => {
    const fetchMock = answer(status)
    vi.stubGlobal('fetch', fetchMock)
    expect(await classifyMediaFailure('/api/media/a')).toBe('gone')
    expect(knownGone('/api/media/a')).toBe(true)
    // The next avatar of the same member does not ask again.
    expect(await classifyMediaFailure('/api/media/a')).toBe('gone')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/media/a', expect.objectContaining({ headers: { Range: 'bytes=0-0' }, credentials: 'same-origin' }))
  })

  it.each([200, 206, 401, 429, 500, 502, 503])('calls a %i a failure that may pass', async status => {
    vi.stubGlobal('fetch', answer(status))
    expect(await classifyMediaFailure('/api/media/b')).toBe('failed')
    expect(knownGone('/api/media/b')).toBe(false)
  })

  it('calls an unreachable server a failure that may pass, and asks again next time', async () => {
    const fetchMock = vi.fn(() => Promise.reject(new TypeError('offline')))
    vi.stubGlobal('fetch', fetchMock)
    expect(await classifyMediaFailure('/api/media/c')).toBe('failed')
    expect(await classifyMediaFailure('/api/media/c')).toBe('failed')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('shares one request among simultaneous failures of an address', async () => {
    const fetchMock = answer(410)
    vi.stubGlobal('fetch', fetchMock)
    const results = await Promise.all([1, 2, 3].map(() => classifyMediaFailure('/api/media/d')))
    expect(results).toEqual(['gone', 'gone', 'gone'])
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
