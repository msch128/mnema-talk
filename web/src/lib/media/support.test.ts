import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mediaSealingSupported, resetMediaSealingProbe } from './support'

function stubTransformSupport(has: { worker: boolean, transform: boolean, channel: boolean }) {
  vi.stubGlobal('Worker', has.worker ? function Worker() {} : undefined)
  vi.stubGlobal('RTCRtpScriptTransform', has.transform ? function RTCRtpScriptTransform() {} : undefined)
  vi.stubGlobal('MessageChannel', has.channel ? function MessageChannel() {} : undefined)
}

function stubEd25519(supported: boolean) {
  const generateKey = vi.fn(() => supported ? Promise.resolve({}) : Promise.reject(new DOMException('unsupported', 'NotSupportedError')))
  vi.stubGlobal('crypto', { subtle: { generateKey } })
}

describe('mediaSealingSupported', () => {
  beforeEach(() => { resetMediaSealingProbe() })
  afterEach(() => { vi.unstubAllGlobals() })

  it('accepts a browser with the frame transform and Ed25519', async () => {
    stubTransformSupport({ worker: true, transform: true, channel: true })
    stubEd25519(true)
    expect(await mediaSealingSupported()).toBe(true)
  })

  it('refuses a browser without the frame transform', async () => {
    stubTransformSupport({ worker: true, transform: false, channel: true })
    stubEd25519(true)
    expect(await mediaSealingSupported()).toBe(false)
  })

  it('refuses a browser without Ed25519', async () => {
    stubTransformSupport({ worker: true, transform: true, channel: true })
    stubEd25519(false)
    expect(await mediaSealingSupported()).toBe(false)
  })

  it('refuses a page without WebCrypto', async () => {
    stubTransformSupport({ worker: true, transform: true, channel: true })
    vi.stubGlobal('crypto', undefined)
    expect(await mediaSealingSupported()).toBe(false)
  })
})
