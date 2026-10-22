/**
 * Firefox lists H.264 among what it can send, but encodes it with a Cisco
 * OpenH264 module it downloads on first use. Where that module is missing or
 * blocked, the encoder never produces a frame and a screen share stays black
 * without any error, so Firefox keeps the VP8 it encodes itself.
 */
function usesOptionalH264Module(userAgent: string): boolean {
  return /\bFirefox\//i.test(userAgent)
}

/** Whether this browser can send H.264 at all (not whether in hardware; the stream diagnostics report that). */
export function canSendH264(userAgent: string = globalThis.navigator?.userAgent ?? ''): boolean {
  if (usesOptionalH264Module(userAgent)) return false
  try {
    const caps = globalThis.RTCRtpSender?.getCapabilities?.('video')
    return !!caps?.codecs?.some(c => /^video\/h264$/i.test(c.mimeType))
  } catch (err) {
    void err
    return false
  }
}
