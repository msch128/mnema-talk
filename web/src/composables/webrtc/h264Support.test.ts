import { afterEach, describe, expect, it, vi } from 'vitest'
import { canSendH264 } from './h264Support'

const CHROME = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36'
const SAFARI = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15'
const FIREFOX = 'Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0'

function stubCodecs(mimeTypes: string[] | 'throws') {
  vi.stubGlobal('RTCRtpSender', {
    getCapabilities: () => {
      if (mimeTypes === 'throws') throw new Error('unsupported')
      return { codecs: mimeTypes.map(mimeType => ({ mimeType })) }
    },
  })
}

describe('canSendH264', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('is true where the browser lists H.264 to send', () => {
    stubCodecs(['video/VP8', 'video/H264'])
    expect(canSendH264(CHROME)).toBe(true)
    expect(canSendH264(SAFARI)).toBe(true)
  })

  it('is false where the browser does not list it', () => {
    stubCodecs(['video/VP8', 'video/VP9'])
    expect(canSendH264(CHROME)).toBe(false)
  })

  it('is false in Firefox, whose H.264 encoder may not be installed', () => {
    stubCodecs(['video/VP8', 'video/H264'])
    expect(canSendH264(FIREFOX)).toBe(false)
  })

  it('is false when the capabilities cannot be read', () => {
    stubCodecs('throws')
    expect(canSendH264(CHROME)).toBe(false)
    vi.stubGlobal('RTCRtpSender', undefined)
    expect(canSendH264(CHROME)).toBe(false)
  })
})
