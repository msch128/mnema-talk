// Connection statistics measured by the browser (RTCPeerConnection.getStats),
// polled for the voice store and, while a screen is shared or watched, summed
// up for the server log.
import { summarizeStats } from '../../lib/rtcStats'
import type { RTCStatsSample } from '../../lib/rtcStats'
import type { useVoiceStore } from '../../stores/voice'
import type { JsonValue } from '../../types/events'
import { streamDiag } from './streamDiag'

type VoiceStore = ReturnType<typeof useVoiceStore>

interface StatsPollingSource {
  connection: () => RTCPeerConnection | null
  /** What keeps this page's own sound out of a shared screen's audio, for the log. */
  screenAudioGuard: () => string | null
}

const POLL_MS = 2000
// Every 10 s the stream diagnostics go out.
const DIAG_EVERY_TICKS = 5

export function createStatsPolling({ connection, screenAudioGuard }: StatsPollingSource) {
  let timer: ReturnType<typeof setInterval> | null = null
  let lastSample: RTCStatsSample | null = null
  let generation = 0
  let ticks = 0
  const lastVideoBytes = new Map<string, number>()

  function stop(voiceStore: VoiceStore) {
    generation++
    if (timer) {
      clearInterval(timer)
      timer = null
    }
    lastSample = null
    if (voiceStore) voiceStore.rtcStats = null
  }

  /** diagSink: where stream diagnostics go (the server log, via the WebSocket). */
  function start(voiceStore: VoiceStore, diagSink: (payload: Record<string, JsonValue | undefined>) => void) {
    stop(voiceStore)
    ticks = 0
    lastVideoBytes.clear()
    const mine = generation
    timer = setInterval(async () => {
      const conn = connection()
      if (!conn) return
      try {
        const report = await conn.getStats()
        // A stopped poll must not restore statistics from a retired connection,
        // including a restarted poll that happens to use the same connection.
        if (connection() !== conn || mine !== generation) return
        const summary = summarizeStats(report.values(), lastSample)
        lastSample = summary.sample
        voiceStore.rtcStats = summary
        if (++ticks % DIAG_EVERY_TICKS === 0 && (voiceStore.isScreenSharing || voiceStore.remoteScreenStream)) {
          const video = streamDiag([...report.values()], lastVideoBytes, POLL_MS * DIAG_EVERY_TICKS / 1000)
          if (video.length) {
            diagSink({ event: 'stream', video, screen_audio: !!voiceStore.hasScreenAudio, own_audio: voiceStore.isScreenSharing ? screenAudioGuard() : undefined, sharing: !!voiceStore.isScreenSharing })
          }
        }
      } catch (err) {
        void err
        // The connection may be closing; the next tick retries.
      }
    }, POLL_MS)
  }

  return { start, stop }
}
