import { describe, expect, it, vi } from 'vitest'

async function load() {
  vi.resetModules()
  return import('./api')
}

describe('media call seam', () => {
  it('prefers an installed call, then a factory, then a call that protects nothing', async () => {
    const api = await load()
    const installed = api.mediaCall()
    expect(installed.protectSender({} as RTCRtpSender)).toBe(false)
    expect(installed.protectIncoming(undefined, 'alice')).toBe(false)
    expect(installed.prepareIncoming({} as RTCRtpReceiver)).toBe(false)
    installed.start('channel', 'alice')
    installed.roster([])
    installed.receive('bob', '')
    installed.resume()
    installed.onKey(() => {})
    installed.onWire(() => {})
    installed.onNotice(() => {})
    installed.onVerification(() => {})
    installed.stop()

    const replacement = await load()
    let built = 0
    const custom = { ...replacement.mediaCall(), start: () => { built += 1 } }
    replacement.installMediaCall(custom)
    expect(replacement.mediaCall()).toBe(custom)
    replacement.mediaCall().start('channel', 'alice')
    expect(built).toBe(1)
    replacement.installMediaCall(null)
    replacement.installMediaCallFactory(() => custom)
    expect(replacement.mediaCall()).toBe(custom)
    expect(replacement.mediaCall().protectSender({} as RTCRtpSender)).toBe(false)
  })
})
