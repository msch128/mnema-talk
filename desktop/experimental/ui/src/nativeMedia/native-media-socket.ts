import { NativeMediaChannel, type NativeMediaBridge, type Role } from './native-media-channel'
export interface MediaSocket {
  readyState: number
  onopen: (() => void) | null
  onmessage: ((event: { data: string }) => void) | null
  onerror: (() => void) | null
  onclose: (() => void) | null
  send(value: string): void
  close(): Promise<void>
}
// Adapts only the fixed qualification signaling shapes. It never constructs a
// renderer WebSocket, sends bearer credentials, or grants arbitrary destinations.
export class NativeMediaSocket implements MediaSocket {
  readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  private channel: NativeMediaChannel
  private stopping?: Promise<void>
  constructor(bridge: NativeMediaBridge, role: Role) {
    this.channel = new NativeMediaChannel(bridge, role, message => {
      if (this.readyState === 1) this.onmessage?.({ data: JSON.stringify(message) })
    }, reason => {
      if (reason === 'error') this.onerror?.()
      this.readyState = 3
      this.onclose?.()
    }, () => { this.readyState = 1; this.onopen?.() })
    queueMicrotask(() => { void this.channel.open().catch(() => { if (this.readyState < 2) this.onerror?.() }) })
  }
  send(value: string): void {
    if (this.readyState !== 1 || value.length > 65536) { this.onerror?.(); return }
    try {
      const frame = JSON.parse(value) as { type: string; payload?: unknown }
      if (frame.type === 'answer') {
        void this.channel.send({ type: 'webrtc_answer', payload: frame.payload as { type: 'answer'; sdp: string } }).catch(() => this.onerror?.())
      } else if (frame.type === 'ice') {
        void this.channel.send({ type: 'webrtc_candidate', payload: frame.payload as RTCIceCandidateInit }).catch(() => this.onerror?.())
      } else { this.onerror?.() }
    } catch { this.onerror?.() }
  }
  close(): Promise<void> {
    if (this.stopping) return this.stopping
    this.readyState = 2
    this.stopping = (async () => {
      await this.channel.close()
      this.readyState = 3
      this.onclose?.()
    })()
    return this.stopping
  }
}
