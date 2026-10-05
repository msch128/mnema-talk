/** Whether this browser can route audio to a chosen output device (setSinkId). */
export function canChooseOutputDevice() {
  return typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype
}
