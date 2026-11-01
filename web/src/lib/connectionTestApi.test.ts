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
  const stream = (chunks: number[]) => new ReadableStream<Uint8Array>({
    start(c) { for (const n of chunks) c.enqueue(new Uint8Array(n)); c.close() }
  })

  it('counts the bytes after the first chunk and times them from the first chunk', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, body: stream([100, 1000, 2000]) })))
    const r = await browserTestDeps.download?.()
    expect(r?.bytes).toBe(3000)
    expect(r?.ms).toBeGreaterThanOrEqual(0)
  })

  it('throws when the server refuses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 429, body: null })))
    await expect(browserTestDeps.download?.()).rejects.toThrow('HTTP 429')
  })
})
