export interface MediaCall {
  start(channelId: string, userId: string): void
  roster(ids: readonly string[]): void
  receive(fromUserId: string, wire: string): void
  /** Sends a missing key package again after the socket comes back. */
  resume(): void
  stop(): void
  onKey(listener: () => void): void
  onWire(listener: (channelId: string, wire: string) => void): void
  protectSender(sender: RTCRtpSender): boolean
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
  protectSender: () => false,
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
