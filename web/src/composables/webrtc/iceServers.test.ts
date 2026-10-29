import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

function config(username: string) {
  return { ice_servers: [{ urls: ['turn:turn.example.com:3478'], username, credential: 'secret' }] }
}

describe('ICE servers', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('reuses the config within the hour and fetches a fresh credential after it', async () => {
    let n = 0
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => config(`cred-${++n}`) }))
    vi.stubGlobal('fetch', fetchMock)
    const { iceServers, loadIceServers } = await import('./iceServers')

    await loadIceServers()
    await loadIceServers()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(iceServers()[0]?.username).toBe('cred-1')

    vi.advanceTimersByTime(59 * 60 * 1000)
    await loadIceServers()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(2 * 60 * 1000)
    await loadIceServers()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(iceServers()[0]?.username).toBe('cred-2')
  })

  it('tries again on the next join after a failed fetch', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, json: async () => null })
      .mockResolvedValueOnce({ ok: true, json: async () => config('cred-1') })
    vi.stubGlobal('fetch', fetchMock)
    const { iceServers, loadIceServers } = await import('./iceServers')

    await loadIceServers()
    expect(iceServers()).toEqual([])
    await loadIceServers()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(iceServers()[0]?.username).toBe('cred-1')
  })

  it('knows a relay is on offer only when the server lists a TURN URL', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ ice_servers: [{ urls: ['stun:stun.example.com:3478'] }] }) })
      .mockResolvedValueOnce({ ok: true, json: async () => config('cred-1') })
    vi.stubGlobal('fetch', fetchMock)
    const { relayOffered, loadIceServers } = await import('./iceServers')

    expect(relayOffered()).toBe(false)
    await loadIceServers()
    expect(relayOffered()).toBe(false)
    vi.advanceTimersByTime(61 * 60 * 1000)
    await loadIceServers()
    expect(relayOffered()).toBe(true)
  })
})
