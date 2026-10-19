import type { NoiseModel } from '../../lib/dfn/dfnTypes'

// Noise mode -> model in lib/noiseSuppressor (null: no AI filter).
export function aiModel(noiseMode: string | null | undefined): NoiseModel | null {
  return noiseMode === 'ai' ? 'dfn3' : noiseMode === 'ai-lite' ? 'gtcrn' : null
}

// Accurate RMS volume calculator (time domain PCM audio)
export function calculateRMSLevel(analyserNode: AnalyserNode | null, buffer: Uint8Array<ArrayBuffer>) {
  if (!analyserNode) return 0
  analyserNode.getByteTimeDomainData(buffer)
  let sum = 0
  for (let i = 0; i < buffer.length; i++) {
    const sample = ((buffer[i] ?? 128) - 128) / 128
    sum += sample * sample
  }
  const rms = Math.sqrt(sum / buffer.length)

  // Map RMS to calibrated 0-100% volume
  // Ambient room noise: rms < 0.005
  // Quiet background speech: rms 0.01 - 0.04 (approx 25-45%)
  // Normal speaking into mic: rms 0.05 - 0.20 (approx 50-80%)
  // Loud speaking: rms > 0.25 (approx 90-100%)
  if (rms < 0.002) return 0
  const db = 20 * Math.log10(rms) // roughly -60dB to 0dB
  const minDb = -60
  const maxDb = -10
  const pct = Math.round(((db - minDb) / (maxDb - minDb)) * 100)
  return Math.max(0, Math.min(100, pct))
}

export function setNodeGain(gainNode: GainNode | null, val: number, time = 0) {
  if (!gainNode?.gain) return
  if (typeof gainNode.gain.setValueAtTime === 'function') {
    try { gainNode.gain.setValueAtTime(val, time) } catch { /* ignore */ }
  } else {
    gainNode.gain.value = val
  }
}

