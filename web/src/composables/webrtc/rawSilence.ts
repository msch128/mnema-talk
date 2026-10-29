import { t } from '../../i18n'
import { useToastStore } from '../../stores/toast'
import type { Cx, RtcState } from './state'

// Digital silence is all-zero samples. Anything above -100 dBFS is a live
// capture, however quiet the room is.
const DIGITAL_SILENCE_PEAK = 1e-5
const SUSTAINED_MS = 15_000
const REPEAT_MS = 120_000

/**
 * Taps the raw capture before the AI filter, input volume and the gate. The
 * capture stays enabled whenever it feeds the audio graph; only the fallback
 * that sends it as it is gates it, and there the metering clone is the raw tap.
 */
export function attachRawTap(st: RtcState, ctx: AudioContext, stream: MediaStream) {
  try {
    const source = ctx.createMediaStreamSource(st.meteringTrack ? new MediaStream([st.meteringTrack]) : stream)
    st.rawAnalyser = ctx.createAnalyser()
    st.rawAnalyser.fftSize = 512
    source.connect(st.rawAnalyser)
  } catch (err) {
    console.warn('[WebRTC] Raw capture tap unavailable:', err)
    st.rawAnalyser = null
  }
}

export function detachRawTap(st: RtcState) {
  st.rawAnalyser = null
}

/** A new call warns afresh. */
export function resetNoAudioWatch(st: RtcState) {
  st.noAudioStart = 0
  st.lastNoAudioWarningToastTime = 0
  st.noAudioDismissed = false
  st.hardwareMuteHinted = false
}

type Cause = 'silence' | 'hardware'

function rawCause(st: RtcState, buffer: Float32Array<ArrayBuffer>): Cause | null {
  const track = st.rawMicStream?.getAudioTracks()[0]
  if (track?.muted) return 'hardware'
  if (!st.rawAnalyser) return null
  st.rawAnalyser.getFloatTimeDomainData(buffer)
  for (const sample of buffer) if (Math.abs(sample) > DIGITAL_SILENCE_PEAK) return null
  return 'silence'
}

function skips({ voice, st }: Cx): boolean {
  if (!voice.warnNoAudioDetected || st.noAudioDismissed || voice.isMuted || voice.isMicTesting) return true
  return voice.inputMode === 'ptt' && !voice.isPttPressed
}

function warn(cx: Cx, cause: Cause) {
  const { st } = cx
  const now = Date.now()
  if (cause === 'hardware') {
    if (st.hardwareMuteHinted) return
    st.hardwareMuteHinted = true
  } else if (now - st.lastNoAudioWarningToastTime <= REPEAT_MS) return
  st.lastNoAudioWarningToastTime = now
  useToastStore().info(t(cause === 'hardware' ? 'audio.micHardwareMutedToast' : 'audio.noAudioDetectedToast'), {
    action: { label: t('audio.noAudioDismiss'), onClick: () => { st.noAudioDismissed = true } }
  })
}

const buffer = new Float32Array(512)

/** Warns when the raw capture (not the processed signal) has been silent for a sustained time. */
export function watchRawSilence(cx: Cx) {
  const { st } = cx
  const cause = skips(cx) ? null : rawCause(st, buffer)
  if (!cause) {
    st.noAudioStart = 0
    return
  }
  if (!st.noAudioStart) st.noAudioStart = Date.now()
  if (Date.now() - st.noAudioStart > SUSTAINED_MS) warn(cx, cause)
}
