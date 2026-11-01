import { STREAM_MAX_BITRATE } from '../../lib/streamQuality'

export type AudioGuard = 'restricted' | 'echo-cancel' | 'none'
export interface BoundSenders { audio?: RTCRtpSender; screen?: RTCRtpSender; camera?: RTCRtpSender; screenAudio?: RTCRtpSender; layers?: RTCRtpSender[]; cameraLayers?: RTCRtpSender[] }
export interface SenderTuning { active?: boolean; maxBitrate?: number; maxFramerate?: number; scaleResolutionDownBy?: number; degradationPreference?: RTCDegradationPreference; priority?: RTCPriorityType; networkPriority?: RTCPriorityType }

// Sender limits (bits per second). The browser's own congestion control still
// lowers the bitrate on a weak uplink; these decide what it gives up while
// doing so and cap the ceiling. A shared screen keeps its resolution and drops
// frames instead (text stays readable; at 60 fps it may trade either, see
// lib/streamQuality), and its cap follows the chosen quality up to
// SCREEN_MAX_BITRATE; a camera may trade either.
export const SCREEN_MAX_BITRATE = STREAM_MAX_BITRATE
export const CAMERA_MAX_BITRATE = 2_500_000

/**
 * Shared system audio would also carry the voices this page plays: the others
 * hear themselves, and with two people sharing it loops. Browsers that can
 * leave this page's own sound out of the capture (restrictOwnAudio, Chrome and
 * Edge 141+) do that and keep the sound untouched; elsewhere echo
 * cancellation removes what the page plays (at some cost to music).
 */
export function screenAudioConstraints(supported = navigator.mediaDevices?.getSupportedConstraints?.() || {}) {
  const base = { autoGainControl: false, noiseSuppression: false }
  if (supported.restrictOwnAudio) return { ...base, echoCancellation: false, restrictOwnAudio: true }
  return { ...base, echoCancellation: true }
}

// What keeps the page's own sound out of a shared screen's audio, for the
// stream diagnostics: restricted, echo-cancel or none.
function ownAudioGuard(track: MediaStreamTrack | null | undefined): AudioGuard {
  const s = track?.getSettings?.() || {}
  if (s.restrictOwnAudio) return 'restricted'
  if (s.echoCancellation) return 'echo-cancel'
  return 'none'
}

// Recognizing a constraint does not prove it was applied to the selected
// monitor's audio. Inspect the actual track before turning off the fallback.
export async function ensureScreenAudioGuard(track: MediaStreamTrack | null | undefined) {
  if (ownAudioGuard(track) === 'none' && typeof track?.applyConstraints === 'function') {
    try {
      await track.applyConstraints({ ...(track.getConstraints?.() || {}), echoCancellation: true })
    } catch (error) {
      /* The selected capture may not support echo cancellation. */
      void error
    }
  }
  return ownAudioGuard(track)
}

function applyEncodingTuning(encoding: RTCRtpEncodingParameters, tuning: SenderTuning): void {
  if (tuning.active !== undefined) encoding.active = tuning.active
  if (tuning.maxBitrate !== undefined) encoding.maxBitrate = tuning.maxBitrate
  if (tuning.maxFramerate !== undefined) encoding.maxFramerate = tuning.maxFramerate
  if (tuning.scaleResolutionDownBy !== undefined) encoding.scaleResolutionDownBy = tuning.scaleResolutionDownBy
  if (tuning.priority) encoding.priority = tuning.priority
  if (tuning.networkPriority) encoding.networkPriority = tuning.networkPriority
}

function prepareSenderParameters(sender: RTCRtpSender, tuning: SenderTuning): RTCRtpSendParameters | null {
  const params = sender.getParameters?.()
  if (!params) return null
  if (!params.encodings?.length) params.encodings = [{}]
  const encoding = params.encodings[0]
  if (!encoding) return null
  applyEncodingTuning(encoding, tuning)
  if (tuning.degradationPreference) params.degradationPreference = tuning.degradationPreference
  return params
}

export async function tuneSender(sender: RTCRtpSender, tuning: SenderTuning = {}) {
  try {
    const params = prepareSenderParameters(sender, tuning)
    if (!params) return
    await sender.setParameters(params)
  } catch (err) {
    // Not every browser accepts every field; the defaults still work.
    console.debug('[WebRTC] Could not tune sender:', err)
  }
}

