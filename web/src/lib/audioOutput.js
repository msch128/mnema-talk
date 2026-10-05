/** Whether this browser can route audio to a chosen output device (setSinkId). */
export function canChooseOutputDevice() {
  return typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype
}

/**
 * Plays `target` (an audio element or an AudioContext) on the output device
 * `deviceId` ('' = system default). setSinkId exists on media elements in
 * Chromium browsers and Firefox and on AudioContext in Chromium; elsewhere
 * the system default is used and this does nothing.
 */
export function applyOutputDevice(target, deviceId) {
  const id = deviceId || ''
  if (typeof target?.setSinkId !== 'function') return
  // An AudioContext reports the default device as '' (or an AudioSinkInfo object).
  const current = typeof target.sinkId === 'string' ? target.sinkId : ''
  if (current === id) return
  try {
    const p = target.setSinkId(id)
    p?.catch?.(err => console.warn('[audio] Output device unavailable:', err))
  } catch (err) {
    console.warn('[audio] Output device unavailable:', err)
  }
}
