import type { MediaVerification } from './verify'
import type { FrameSource } from './frame'

/**
 * Keys changed in a way the member did not ask for: 'removed' by someone else
 * in the call, 'absent' after this browser itself slept and was dropped for silence.
 * 'outdated' is something else: a member sends media in the retired frame
 * format (an old client), which this one drops.
 */
export type MediaNotice = 'removed' | 'absent' | 'outdated'

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
  /**
   * Seals what a sender sends. `source` is what the line carries (microphone,
   * camera, screen, screen sound); it is bound into every frame. `layer` (0..3)
   * marks a line of a screen share's layers, whose frames then carry that
   * layer in their stream ID. `dtx` says the line's Opus encoder uses DTX:
   * the empty frames it hands over while silent are then not sent.
   */
  protectSender(sender: RTCRtpSender, source: FrameSource, layer?: number, dtx?: boolean): boolean
  /**
   * Arms a receiver before its line receives anything. A browser applies a
   * receiver transform only if it was set before then, and the publisher of a
   * line is not known until the track event, so a prepared receiver opens
   * nothing until protectIncoming names it.
   */
  prepareIncoming(receiver: RTCRtpReceiver): boolean
  /** `source` is what the SFU says this line carries; a frame that names another source does not open. */
  protectIncoming(receiver: RTCRtpReceiver | undefined, senderId: string, source: FrameSource): boolean
  /**
   * Whether this browser can seal call media. Asked before a join; when it
   * answers false the join is refused. A call without this method is taken to
   * be able, and the per-line protect calls above still refuse unsealed media.
   */
  canSeal?(): Promise<boolean>
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
