// Connection statistics measured by the browser (RTCPeerConnection.getStats),
// polled for the voice store and, while a screen is shared or watched, summed
// up for the server log.
import { watch } from 'vue'
import { summarizeStats } from '../../lib/rtcStats'
import type { RTCStatsSample } from '../../lib/rtcStats'
import type { MicState, SendLine, SendLines } from '../../lib/rtcFlows'
import { pageHidden, visibleInterval } from '../../lib/pageVisibility'
import type { useVoiceStore } from '../../stores/voice'
import type { JsonValue } from '../../types/events'
import { streamDiag } from './streamDiag'

type SenderLines = ReadonlyArray<readonly [SendLine, RTCRtpSender | null]>
type VoiceStore = ReturnType<typeof useVoiceStore>

interface StatsPollingSource {
  connection: () => RTCPeerConnection | null
  /** What keeps this page's own sound out of a shared screen's audio, for the log. */
  screenAudioGuard: () => string | null
  /** The senders of the lines this client publishes on, by what they carry. */
  senders: () => SenderLines
}

// The connection panel shows live numbers; closed, the poll only keeps the
// connection state and the stream diagnostics.
const POLL_MS = 2000
const PANEL_POLL_MS = 1000
// Nobody reads the numbers in a hidden page; the connected flag only has to stay roughly true.
const HIDDEN_POLL_MS = 10_000
// Every 10 s the stream diagnostics go out.
const DIAG_EVERY_MS = 10_000

function sharesOrWatchesScreen(voiceStore: VoiceStore): boolean {
  return Boolean(voiceStore.isScreenSharing || voiceStore.remoteScreenStream)
}

/** The mids of my publishing lines, so a stream is read as what its line carries. */
export function sendLinesOf(conn: RTCPeerConnection, senders: SenderLines): SendLines {
  const lines = new Map<string, SendLine>()
  for (const transceiver of conn.getTransceivers?.() ?? []) {
    if (!transceiver.mid) continue
    for (const [line, sender] of senders) {
      if (sender && sender === transceiver.sender) lines.set(transceiver.mid, line)
    }
  }
  return lines
}

/** Whether the microphone line is sending now: a muted or gated track sends silence or nothing. */
export function micStateOf(sender: RTCRtpSender | null | undefined): MicState {
  const track = sender?.track
  if (!track || track.readyState === 'ended') return 'none'
  return track.enabled ? 'sending' : 'paused'
}

function emitStreamDiagnostics(
  report: RTCStatsReport,
  voiceStore: VoiceStore,
  lastVideoBytes: Map<string, number>,
  screenAudioGuard: () => string | null,
  diagSink: (payload: Record<string, JsonValue | undefined>) => void,
) {
  const video = streamDiag([...report.values()], lastVideoBytes, DIAG_EVERY_MS / 1000)
  if (!video.length) return
  diagSink({
    event: 'stream',
    video,
    screen_audio: Boolean(voiceStore.hasScreenAudio),
    own_audio: voiceStore.isScreenSharing ? screenAudioGuard() : undefined,
    sharing: Boolean(voiceStore.isScreenSharing),
  })
}

export function createStatsPolling({ connection, screenAudioGuard, senders }: StatsPollingSource) {
  let stopTimer: (() => void) | null = null
  let stopWatching: (() => void) | null = null
  let lastSample: RTCStatsSample | null = null
  let generation = 0
  const lastVideoBytes = new Map<string, number>()

  function stop(voiceStore: VoiceStore) {
    generation++
    stopTimer?.()
    stopTimer = null
    stopWatching?.()
    stopWatching = null
    lastSample = null
    if (voiceStore) voiceStore.rtcStats = null
  }

  /** diagSink: where stream diagnostics go (the server log, via the WebSocket). */
  function start(voiceStore: VoiceStore, diagSink: (payload: Record<string, JsonValue | undefined>) => void) {
    stop(voiceStore)
    lastVideoBytes.clear()
    const mine = generation
    let elapsedMs = 0
    let nextDiagMs = DIAG_EVERY_MS

    async function tick(period: number) {
      const conn = connection()
      if (!conn) return
      try {
        const report = await conn.getStats()
        // A stopped poll must not restore statistics from a retired connection,
        // including a restarted poll that happens to use the same connection.
        if (connection() !== conn || mine !== generation) return
        const lineSenders = senders()
        const lines = sendLinesOf(conn, lineSenders)
        const summary = summarizeStats(report.values(), lastSample, { lines, micState: micStateOf(lineSenders.find(([line]) => line === 'mic')?.[1]) })
        lastSample = summary.sample
        voiceStore.rtcStats = summary
        elapsedMs += period
        if (elapsedMs >= nextDiagMs) {
          nextDiagMs += DIAG_EVERY_MS
          // A hidden page receives no video, and the ticks are not DIAG-spaced.
          if (!pageHidden.value && sharesOrWatchesScreen(voiceStore)) emitStreamDiagnostics(report, voiceStore, lastVideoBytes, screenAudioGuard, diagSink)
        }
      } catch (err) {
        void err
        // The connection may be closing; the next tick retries.
      }
    }

    // Open, the panel samples every second; the rate of a sample is always
    // measured against the one before, whatever the period was.
    function schedule(panelOpen: boolean) {
      stopTimer?.()
      const period = panelOpen ? PANEL_POLL_MS : POLL_MS
      stopTimer = visibleInterval(() => void tick(pageHidden.value ? HIDDEN_POLL_MS : period), period, { hiddenMs: HIDDEN_POLL_MS, catchUp: false })
      if (panelOpen) void tick(0)
    }
    schedule(Boolean(voiceStore.showStatsModal))
    stopWatching = watch(() => voiceStore.showStatsModal, open => schedule(Boolean(open)))
  }

  return { start, stop }
}
