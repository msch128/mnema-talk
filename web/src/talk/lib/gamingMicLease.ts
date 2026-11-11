// Schedule the deadline on the audio rendering clock. A renderer-thread stall
// cannot extend an already granted desktop PTT microphone lease.
export function gamingMicLease(gain: GainNode, context: AudioContext, ptt: boolean, pressed: boolean): void {
  const now = context.currentTime
  gain.gain.cancelScheduledValues(now)
  gain.gain.setValueAtTime(ptt && !pressed ? 0 : 1, now)
  if (ptt && pressed) gain.gain.setValueAtTime(0, now + 0.3)
}
