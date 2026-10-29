import { describe, it, expect } from 'vitest'
import { summarizeStats } from './rtcStats'
import { flowRows, micStatus } from './rtcFlowRows'
import { setLocale } from '../i18n'
import type { SendLines } from './rtcFlows'
import type { MediaStat } from './mediaStats'

// The SFU's lines: mic 0, screen 1, camera 2, screen sound 3.
const LINES: SendLines = new Map([['0', 'mic'], ['1', 'screen'], ['2', 'camera'], ['3', 'screenAudio']])

function pair(timestamp: number, bytesSent: number): MediaStat {
  return { id: 'cp', type: 'candidate-pair', state: 'succeeded', nominated: true, localCandidateId: 'l', remoteCandidateId: 'r', bytesSent, bytesReceived: 0, timestamp }
}

// One report as a browser lists it: the screen sound's line comes after the
// microphone's and exists before it sends anything.
function report(timestamp: number, mic: { packets: number; bytes: number }, extra: MediaStat[] = []): MediaStat[] {
  return [
    pair(timestamp, mic.bytes),
    { id: 'l', type: 'local-candidate', candidateType: 'relay', protocol: 'udp' },
    { id: 'r', type: 'remote-candidate', candidateType: 'host', protocol: 'udp' },
    { id: 'o-mic', type: 'outbound-rtp', kind: 'audio', mid: '0', packetsSent: mic.packets, bytesSent: mic.bytes },
    { id: 'o-sa', type: 'outbound-rtp', kind: 'audio', mid: '3', packetsSent: 0, bytesSent: 0 },
    ...extra
  ]
}

describe('packets sent per line', () => {
  it('counts the microphone, not the idle screen-sound line listed after it', () => {
    const s = summarizeStats(report(1000, { packets: 500, bytes: 40_000 }), null, { lines: LINES, micState: 'sending' })
    expect(s.packetsSent).toBe(500)
    expect(s.flows.mic?.packets).toBe(500)
    expect(s.flows.screenAudio?.packets).toBe(0)
  })

  it('shows packets, bytes and rate that grow from sample to sample', () => {
    const first = summarizeStats(report(1000, { packets: 500, bytes: 40_000 }), null, { lines: LINES })
    const second = summarizeStats(report(2000, { packets: 550, bytes: 46_000 }), first.sample, { lines: LINES })
    const third = summarizeStats(report(3000, { packets: 600, bytes: 52_000 }), second.sample, { lines: LINES })
    expect([first, second, third].map(s => s.packetsSent)).toEqual([500, 550, 600])
    expect(first.flows.mic?.kbps).toBeNull()
    expect(second.flows.mic?.kbps).toBe(48)
    expect(third.flows.mic?.bytes).toBe(52_000)
    expect(third.sendKbps).toBe(48)
  })

  it('starts again on a new connection without a negative rate', () => {
    const before = summarizeStats(report(9000, { packets: 4000, bytes: 300_000 }), null, { lines: LINES })
    // A rejoin: new counters, and the poll drops the old sample.
    const after = summarizeStats(report(1000, { packets: 20, bytes: 1600 }), null, { lines: LINES })
    expect(after.packetsSent).toBe(20)
    expect(after.flows.mic?.kbps).toBeNull()
    // Even a stale sample must not give a negative rate.
    const stale = summarizeStats(report(10_000, { packets: 20, bytes: 1600 }), before.sample, { lines: LINES })
    expect(stale.flows.mic?.kbps).toBe(0)
  })

  it('keeps microphone, camera, screen layers and screen sound apart', () => {
    const extra: MediaStat[] = [
      { id: 'o-cam', type: 'outbound-rtp', kind: 'video', mid: '2', packetsSent: 300, bytesSent: 90_000 },
      { id: 'o-scr', type: 'outbound-rtp', kind: 'video', mid: '1', packetsSent: 1000, bytesSent: 800_000 },
      { id: 'o-scr2', type: 'outbound-rtp', kind: 'video', mid: '1', packetsSent: 200, bytesSent: 50_000 },
      { id: 'o-stale', type: 'outbound-rtp', kind: 'video', mid: '9', packetsSent: 77, bytesSent: 77 },
      { id: 'o-sa', type: 'outbound-rtp', kind: 'audio', mid: '3', packetsSent: 40, bytesSent: 3000 }
    ]
    const s = summarizeStats(report(1000, { packets: 500, bytes: 40_000 }, extra.slice(0, 4)).filter(r => r.id !== 'o-sa').concat(extra.slice(4)), null, { lines: LINES })
    expect(s.flows.mic?.packets).toBe(500)
    expect(s.flows.camera?.packets).toBe(300)
    expect(s.flows.screen).toMatchObject({ packets: 1200, bytes: 850_000 })
    expect(s.flows.screenAudio?.packets).toBe(40)
  })

  it('reads streams by kind when the browser names no line', () => {
    const s = summarizeStats([
      { id: 'a', type: 'outbound-rtp', kind: 'audio', packetsSent: 5 },
      { id: 'v', type: 'outbound-rtp', kind: 'video', contentType: 'screenshare', packetsSent: 6 },
      { id: 'w', type: 'outbound-rtp', kind: 'video', packetsSent: 7 }
    ])
    expect([s.flows.mic?.packets, s.flows.screen?.packets, s.flows.camera?.packets]).toEqual([5, 6, 7])
  })

  it('counts what is received per kind with its loss', () => {
    const s = summarizeStats([
      { id: 'i1', type: 'inbound-rtp', kind: 'audio', packetsReceived: 100, packetsLost: 2, bytesReceived: 8000 },
      { id: 'i2', type: 'inbound-rtp', kind: 'audio', packetsReceived: 50, bytesReceived: 4000 },
      { id: 'i3', type: 'inbound-rtp', kind: 'video', packetsReceived: 900, packetsLost: 9, bytesReceived: 700_000 }
    ])
    expect(s.flows.audioIn).toMatchObject({ packets: 150, bytes: 12_000, lost: 2 })
    expect(s.flows.videoIn).toMatchObject({ packets: 900, lost: 9 })
  })
})

describe('panel lines', () => {
  it('labels the lines in English and German', () => {
    const s = summarizeStats(report(1000, { packets: 1234, bytes: 98_765 }), null, { lines: LINES, micState: 'sending' })
    setLocale('en')
    expect(flowRows(s)).toEqual([['Audio sent', '1,234 packets (–) · 96.5 KB', 'mic']])
    setLocale('de')
    expect(flowRows(s)[0]?.[0]).toBe('Audio gesendet')
    expect(flowRows(s)[0]?.[1]).toContain('1.234 Pakete')
    setLocale('en')
  })

  it('shows the rate once there is one and hides lines that carry nothing', () => {
    setLocale('en')
    const first = summarizeStats(report(1000, { packets: 500, bytes: 40_000 }), null, { lines: LINES })
    const second = summarizeStats(report(2000, { packets: 550, bytes: 46_000 }), first.sample, { lines: LINES })
    expect(flowRows(second)).toEqual([['Audio sent', '550 packets (48 kbit/s) · 44.9 KB', 'mic']])
    expect(flowRows(null)).toEqual([])
  })

  it('says why the microphone stands still', () => {
    setLocale('en')
    expect(micStatus('sending', false, 'activity')).toBe('Sending')
    expect(micStatus('paused', false, 'activity')).toBe('Paused (voice gate: silent)')
    expect(micStatus('paused', false, 'ptt')).toBe('Paused (push-to-talk not pressed)')
    expect(micStatus('paused', true, 'activity')).toBe('Muted, nothing is sent')
    expect(micStatus('none', false, 'activity')).toBe('No microphone')
  })
})
