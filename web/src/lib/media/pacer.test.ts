import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HANDSHAKE_BYTES_PER_SEC, HANDSHAKES_PER_SEC, WirePacer } from './pacer'

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('handshake pacing', () => {
  it('sends a burst in order, never more per second than the server relays', () => {
    const pacer = new WirePacer()
    const sent: { n: number, at: number }[] = []
    for (let n = 0; n < 20; n++) pacer.push('w', () => sent.push({ n, at: Date.now() }))
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC)
    vi.advanceTimersByTime(5000)
    expect(sent.map(item => item.n)).toEqual([...Array(20).keys()])
    for (const item of sent) {
      expect(sent.filter(other => other.at >= item.at && other.at < item.at + 1000).length).toBeLessThanOrEqual(HANDSHAKES_PER_SEC)
    }
  })

  it('holds back by bytes as well, but always lets one oversized wire through an empty second', () => {
    const pacer = new WirePacer()
    const big = 'x'.repeat(HANDSHAKE_BYTES_PER_SEC)
    const sent: number[] = []
    pacer.push(big, () => sent.push(1))
    pacer.push('small', () => sent.push(2))
    expect(sent).toEqual([1])
    vi.advanceTimersByTime(1001)
    expect(sent).toEqual([1, 2])
  })

  it('drops what is still waiting when the call moves on', () => {
    const pacer = new WirePacer()
    const sent: number[] = []
    for (let n = 0; n < HANDSHAKES_PER_SEC + 3; n++) pacer.push('w', () => sent.push(n))
    pacer.clear()
    vi.advanceTimersByTime(5000)
    expect(sent).toHaveLength(HANDSHAKES_PER_SEC)
    pacer.clear()
    pacer.push('w', () => sent.push(99))
    expect(sent.at(-1)).toBe(99)
  })
})
