import { describe, expect, it } from 'vitest'
import { isLocalAddress, screenViewersFor, verdictOf, type ConnectionReport, type PathResult } from './connectionVerdict'

const ok = (remoteAddress = '203.0.113.7'): PathResult => ({ outcome: 'ok', pair: { localType: 'srflx', remoteType: 'host', remoteAddress } })
const fail: PathResult = { outcome: 'fail' }
const none: PathResult = { outcome: 'unavailable' }

function report(over: Partial<ConnectionReport> = {}): ConnectionReport {
  return {
    candidates: { host: 1, srflx: 1, relay: 2 }, stunOffered: true, turnOffered: true,
    direct: fail, relayUdp: fail, relayTcp: fail, voiceOff: false, limited: false, ...over
  }
}

describe('verdictOf', () => {
  it('says the direct connection works when the voice port answered', () => {
    expect(verdictOf(report({ direct: ok(), relayUdp: ok(), relayTcp: ok() }))).toEqual({ kind: 'direct', cause: 'ok' })
  })

  it('warns that a direct connection on the same network proves little about the way in', () => {
    for (const addr of ['192.168.1.20', '10.0.0.5', '172.20.1.1', 'fd12::1']) {
      expect(verdictOf(report({ direct: ok(addr) })), addr).toEqual({ kind: 'direct', cause: 'same_network' })
    }
  })

  it('says only the relay works, and blames the voice port when UDP to the relay passes', () => {
    expect(verdictOf(report({ relayUdp: ok(), relayTcp: ok() }))).toEqual({ kind: 'relay_only', cause: 'udp_not_forwarded' })
  })

  it('blames a network that blocks UDP when only the relay over TCP works', () => {
    expect(verdictOf(report({ relayTcp: ok() }))).toEqual({ kind: 'relay_only', cause: 'udp_blocked' })
  })

  it('says nothing works, with the likely cause from what was found', () => {
    expect(verdictOf(report({ voiceOff: true }))).toEqual({ kind: 'none', cause: 'voice_off' })
    expect(verdictOf(report({ turnOffered: false, relayUdp: none, relayTcp: none }))).toEqual({ kind: 'none', cause: 'no_relay_offered' })
    expect(verdictOf(report({ candidates: { host: 2, srflx: 0, relay: 0 } }))).toEqual({ kind: 'none', cause: 'only_local_addresses' })
    expect(verdictOf(report({ candidates: { host: 2, srflx: 1, relay: 0 } }))).toEqual({ kind: 'none', cause: 'relay_unreachable' })
    expect(verdictOf(report())).toEqual({ kind: 'none', cause: 'udp_not_forwarded' })
  })

  it('is inconclusive when the server refused the test as too frequent and nothing connected', () => {
    expect(verdictOf(report({ limited: true }))).toEqual({ kind: 'inconclusive', cause: 'limited' })
    // A path that did connect before the limit hit still counts.
    expect(verdictOf(report({ limited: true, direct: ok() })).kind).toBe('direct')
  })
})

describe('isLocalAddress', () => {
  it('knows private, loopback, link-local and shared ranges', () => {
    for (const a of ['10.1.2.3', '127.0.0.1', '169.254.1.1', '192.168.0.9', '172.16.0.1', '172.31.255.1', '100.64.0.1', '100.127.1.1', '::1', 'fe80::1', 'fd00::2', 'abc.local']) {
      expect(isLocalAddress(a), a).toBe(true)
    }
    for (const a of ['8.8.8.8', '203.0.113.7', '172.32.0.1', '100.128.0.1', '2a00:1450::1']) {
      expect(isLocalAddress(a), a).toBe(false)
    }
  })
})

describe('screenViewersFor', () => {
  it('divides the upload by what one screen watcher needs (1.5 Mbit/s plus 30 %)', () => {
    expect(screenViewersFor(0)).toBe(0)
    expect(screenViewersFor(1.9)).toBe(0)
    expect(screenViewersFor(1.95)).toBe(1)
    expect(screenViewersFor(100)).toBe(51)
    expect(screenViewersFor(-5)).toBe(0)
  })
})
