import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { createStatsPolling, micStateOf, sendLinesOf } from './statsPolling'
import { useVoiceStore } from '../../stores/voice'
import type { MediaStat } from '../../lib/mediaStats'

type Sender = RTCRtpSender

function sender(track: Partial<MediaStreamTrack> | null): Sender {
  return { track } as unknown as Sender
}

// A connection whose report grows by 50 packets (6 kB) per second on the mic line.
function fakeConnection(micSender: Sender, screenAudioSender: Sender) {
  let seconds = 0
  const conn = {
    getTransceivers: () => [{ mid: '0', sender: micSender }, { mid: '3', sender: screenAudioSender }],
    getStats: vi.fn(async () => {
      seconds++
      const stats: MediaStat[] = [
        { id: 'cp', type: 'candidate-pair', state: 'succeeded', nominated: true, timestamp: seconds * 1000, bytesSent: seconds * 6000, bytesReceived: 0 },
        { id: 'mic', type: 'outbound-rtp', kind: 'audio', mid: '0', packetsSent: seconds * 50, bytesSent: seconds * 6000 },
        { id: 'sa', type: 'outbound-rtp', kind: 'audio', mid: '3', packetsSent: 0, bytesSent: 0 }
      ]
      return new Map(stats.map(s => [s.id, s]))
    })
  }
  return conn
}

describe('stats polling', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  function setup(track: Partial<MediaStreamTrack> = { enabled: true, readyState: 'live' }) {
    const mic = sender(track)
    const screenAudio = sender(null)
    const conn = fakeConnection(mic, screenAudio)
    let current: unknown = conn
    const polling = createStatsPolling({
      connection: () => current as RTCPeerConnection,
      screenAudioGuard: () => null,
      senders: () => [['mic', mic], ['screenAudio', screenAudio]]
    })
    const voice = useVoiceStore()
    return { conn, polling, voice, replace: (next: unknown) => { current = next } }
  }

  it('samples every second while the panel is open and shows packets that rise', async () => {
    const { conn, polling, voice } = setup()
    voice.showStatsModal = true
    polling.start(voice, () => {})
    await vi.advanceTimersByTimeAsync(0)
    const seen: number[] = []
    for (let i = 0; i < 3; i++) {
      await vi.advanceTimersByTimeAsync(1000)
      seen.push(voice.rtcStats?.packetsSent ?? -1)
    }
    expect(conn.getStats).toHaveBeenCalledTimes(4)
    expect(seen[1]).toBeGreaterThan(seen[0] ?? 0)
    expect(seen[2]).toBeGreaterThan(seen[1] ?? 0)
    expect(voice.rtcStats?.flows.mic?.kbps).toBe(48)
    expect(voice.rtcStats?.micState).toBe('sending')
    polling.stop(voice)
  })

  it('polls slowly while the panel is closed, and speeds up when it opens and slows when it closes', async () => {
    const { conn, polling, voice } = setup()
    polling.start(voice, () => {})
    await vi.advanceTimersByTimeAsync(4000)
    expect(conn.getStats).toHaveBeenCalledTimes(2)
    voice.showStatsModal = true
    await nextTick()
    await vi.advanceTimersByTimeAsync(2000)
    expect(conn.getStats).toHaveBeenCalledTimes(5) // at once, then twice more
    voice.showStatsModal = false
    await nextTick()
    await vi.advanceTimersByTimeAsync(4000)
    expect(conn.getStats).toHaveBeenCalledTimes(7)
    polling.stop(voice)
    await vi.advanceTimersByTimeAsync(5000)
    expect(conn.getStats).toHaveBeenCalledTimes(7)
    expect(voice.rtcStats).toBeNull()
  })

  it('follows a rejoin to a new connection with its own counters', async () => {
    const { polling, voice, replace } = setup()
    voice.showStatsModal = true
    polling.start(voice, () => {})
    await vi.advanceTimersByTimeAsync(3000)
    const before = voice.rtcStats?.packetsSent ?? 0
    expect(before).toBeGreaterThan(100)
    const mic = sender({ enabled: true, readyState: 'live' })
    const fresh = fakeConnection(mic, sender(null))
    replace(fresh)
    polling.start(voice, () => {})
    await vi.advanceTimersByTimeAsync(1000)
    expect(voice.rtcStats?.packetsSent).toBeLessThan(before)
    expect(fresh.getStats).toHaveBeenCalled()
    polling.stop(voice)
  })

  it('reports a gated microphone as paused while the counters stand still', async () => {
    const track = { enabled: false, readyState: 'live' } as MediaStreamTrack
    const { polling, voice } = setup(track)
    voice.showStatsModal = true
    polling.start(voice, () => {})
    await vi.advanceTimersByTimeAsync(1000)
    expect(voice.rtcStats?.micState).toBe('paused')
    polling.stop(voice)
  })
})

describe('line helpers', () => {
  it('maps mids to what the sender carries and ignores empty slots', () => {
    const mic = sender(null)
    const conn = { getTransceivers: () => [{ mid: '0', sender: mic }, { mid: null, sender: sender(null) }] } as unknown as RTCPeerConnection
    expect([...sendLinesOf(conn, [['mic', mic], ['screen', null]])]).toEqual([['0', 'mic']])
    expect(sendLinesOf({} as RTCPeerConnection, [['mic', mic]]).size).toBe(0)
  })

  it('tells sending, paused and no microphone', () => {
    expect(micStateOf(sender({ enabled: true, readyState: 'live' }))).toBe('sending')
    expect(micStateOf(sender({ enabled: false, readyState: 'live' }))).toBe('paused')
    expect(micStateOf(sender({ enabled: true, readyState: 'ended' }))).toBe('none')
    expect(micStateOf(sender(null))).toBe('none')
    expect(micStateOf(undefined)).toBe('none')
  })
})
