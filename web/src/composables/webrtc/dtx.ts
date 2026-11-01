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
