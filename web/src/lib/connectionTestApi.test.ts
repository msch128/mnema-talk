import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApiOptions } from './api'
import { browserTestDeps } from './connectionTestApi'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('./api', () => ({ api: (path: string, options?: ApiOptions) => apiMock(path, options) }))

const loadMock = vi.fn<() => Promise<void>>()
let servers: RTCIceServer[] = []
vi.mock('../composables/webrtc/iceServers', () => ({ loadIceServers: () => loadMock(), iceServers: () => servers }))

beforeEach(() => { apiMock.mockReset(); loadMock.mockReset(); loadMock.mockResolvedValue(undefined); servers = [] })
afterEach(() => vi.unstubAllGlobals())

describe('browserTestDeps', () => {
  it('hands out the ICE servers the server offered, after loading them', async () => {
    servers = [{ urls: 'stun:stun.example:3478' }]
    expect(await browserTestDeps.iceServers()).toEqual(servers)
    expect(loadMock).toHaveBeenCalledOnce()
  })

  it('sends the offer to the probe endpoint and returns the answer', async () => {
    apiMock.mockResolvedValue({ sdp: 'v=0 answer' })
    expect(await browserTestDeps.probe('v=0 offer')).toBe('v=0 answer')
    const [path, options] = apiMock.mock.calls[0] ?? []
    expect(path).toBe('/api/webrtc/probe')
    expect(options).toMatchObject({ method: 'POST', json: { sdp: 'v=0 offer' } })
  })

  it('creates a peer connection with the given configuration', () => {
    const made: RTCConfiguration[] = []
    vi.stubGlobal('RTCPeerConnection', class { constructor(c: RTCConfiguration) { made.push(c) } })
    browserTestDeps.createPeer({ iceTransportPolicy: 'relay' })
    expect(made).toEqual([{ iceTransportPolicy: 'relay' }])
  })
})

describe('browserTestDeps.download', () => {
  const stream = (chunks: number[], fail = false) => new ReadableStream<Uint8Array>({
    // One chunk per read; the error (when asked for) comes after the last chunk was read.
    pull(c) {
      const n = chunks.shift()
      if (n !== undefined) c.enqueue(new Uint8Array(n))
      else if (fail) c.error(new Error('connection reset'))
      else c.close()
    }
  })
  const answer = (body: ReadableStream<Uint8Array> | null, status = 200) => vi.fn(async () => ({ ok: status === 200, status, body }))

  it('counts the bytes after the first chunk and times them from the first chunk', async () => {
    vi.stubGlobal('fetch', answer(stream([100, 1000, 2000])))
    const r = await browserTestDeps.download?.()
    expect(r?.bytes).toBe(3000)
    expect(r?.ms).toBeGreaterThanOrEqual(0)
  })

  it('keeps what arrived when the stream is cut short, and fails when nothing did', async () => {
    vi.stubGlobal('fetch', answer(stream([100, 4000], true)))
    expect((await browserTestDeps.download?.())?.bytes).toBe(4000)
    vi.stubGlobal('fetch', answer(stream([], true)))
    await expect(browserTestDeps.download?.()).rejects.toThrow('no data')
  })

  it('tells the refusals apart and throws on other errors', async () => {
    vi.stubGlobal('fetch', answer(null, 429))
    await expect(browserTestDeps.download?.()).rejects.toMatchObject({ kind: 'limited' })
    vi.stubGlobal('fetch', answer(null, 503))
    await expect(browserTestDeps.download?.()).rejects.toMatchObject({ kind: 'busy' })
    vi.stubGlobal('fetch', answer(null, 500))
    await expect(browserTestDeps.download?.()).rejects.toThrow('HTTP 500')
  })

  it('gives fetch a signal that ends with the caller and with a timeout', async () => {
    const fetchMock = answer(stream([100, 1000]))
    vi.stubGlobal('fetch', fetchMock)
    const caller = new AbortController()
    await browserTestDeps.download?.(caller.signal)
    const used = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1].signal as AbortSignal
    expect(used.aborted).toBe(false)
    caller.abort()
    expect(used.aborted).toBe(true)
  })
})
