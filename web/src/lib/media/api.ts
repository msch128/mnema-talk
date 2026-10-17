import type { MediaVerification } from './verify'

/**
 * Keys changed in a way the member did not ask for: 'removed' by someone else
 * in the call, 'absent' after this browser itself slept and was dropped for silence.
 */
export type MediaNotice = 'removed' | 'absent'

export interface MediaCall {
  start(channelId: string, userId: string): void
  roster(ids: readonly string[]): void
  receive(fromUserId: string, wire: string): void
  /** Sends a missing key package again after the socket comes back. */
  resume(): void
  stop(): void
  onKey(listener: () => void): void
  onWire(listener: (channelId: string, wire: string) => void): void
  onNotice(listener: (notice: MediaNotice) => void): void
  /** The safety code of the keys in use, or null while there are none. */
  onVerification(listener: (verification: MediaVerification | null) => void): void
  protectSender(sender: RTCRtpSender): boolean
  /**
   * Arms a receiver before its line receives anything. A browser applies a
   * receiver transform only if it was set before then, and the publisher of a
   * line is not known until the track event, so a prepared receiver opens
   * nothing until protectIncoming names it.
   */
  prepareIncoming(receiver: RTCRtpReceiver): boolean
  protectIncoming(receiver: RTCRtpReceiver | undefined, senderId: string): boolean
}

const disabled: MediaCall = {
  start() {},
  roster() {},
  receive() {},
  resume() {},
  stop() {},
  onKey() {},
  onWire() {},
  onNotice() {},
  onVerification() {},
  protectSender: () => false,
  prepareIncoming: () => false,
  protectIncoming: () => false,
}

let installed: MediaCall | null = null
let factory: (() => MediaCall) | null = null

export function installMediaCall(call: MediaCall | null) {
  installed = call
}

export function installMediaCallFactory(next: () => MediaCall) {
  factory = next
}

export function mediaCall(): MediaCall {
  if (installed) return installed
  installed = factory ? factory() : disabled
  return installed
}
