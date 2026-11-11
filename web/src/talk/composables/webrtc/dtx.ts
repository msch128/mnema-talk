import type { Cx } from './state'

/**
 * Whether the microphone asks for discontinuous transmission (Opus DTX): the
 * setting is on and the noise filter is not off. Off is the setting for studio
 * microphones and for music; the encoder takes a steady or quiet musical sound
 * for silence and sends comfort noise instead of it.
 */
export function wantsDtx(voice: Pick<Cx['voice'], 'silenceSaving' | 'noiseMode'>): boolean {
  return voice.silenceSaving && voice.noiseMode !== 'off'
}

/**
 * Whether this call's microphone uses DTX, decided once per call (with the
 * first offer) and kept. Three things must agree: the offer asks for it, the
 * encoder then sends empty frames while silent, and the media worker drops
 * them. The worker is told once, when the line is bound, so a setting or the
 * noise filter changed mid-call applies from the next Talk, as the setting says.
 */
export function callDtx(cx: Pick<Cx, 'voice' | 'st'>): boolean {
  cx.st.dtx ??= wantsDtx(cx.voice)
  return cx.st.dtx
}
