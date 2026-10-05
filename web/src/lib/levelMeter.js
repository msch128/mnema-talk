// Helpers for the input level meter in the audio settings. The meter has to
// stay readable when the threshold sits above the current level, otherwise
// there is nothing to calibrate the noise gate against.

// Threshold the voice-activity gate uses while "Automatisch ermitteln" is on
// (see setupSpeakingDetection in composables/useWebRTC.js).
export const AUTO_THRESHOLD = 25

// How long the peak marker stays at the loudest recent level.
export const PEAK_HOLD_MS = 1500

/** The threshold the noise gate actually applies, as a number 0-100. */
export function effectiveThreshold({ autoSensitivity, sensitivityThreshold }) {
  return autoSensitivity ? AUTO_THRESHOLD : Number(sensitivityThreshold)
}

/**
 * Returns a function (level, now) => peak that follows rising levels at once
 * and keeps the highest level for PEAK_HOLD_MS before dropping back.
 */
export function createPeakHold(holdMs = PEAK_HOLD_MS) {
  let peak = 0
  let peakAt = -Infinity
  return (level, now = Date.now()) => {
    if (level >= peak || now - peakAt > holdMs) {
      peak = level
      peakAt = now
    }
    return peak
  }
}
