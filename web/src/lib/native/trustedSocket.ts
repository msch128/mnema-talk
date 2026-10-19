import { Channel, invoke } from '@tauri-apps/api/core'

/**
 * WebSocket for the trusted desktop client. The page cannot reach the server
 * directly (its origin is the host's own), so the host opens the connection,
 * adds the session cookie and the instance's Origin, and relays text frames.
 * The surface is the part of WebSocket the chat store uses.
 */
type HostEvent =
  | { id: number; kind: 'open' }
  | { id: number; kind: 'message'; data: string }
  | { id: number; kind: 'close' }

export class TrustedSocket {
  readonly CONNECTING = 0
  readonly OPEN = 1
  readonly CLOSING = 2
  readonly CLOSED = 3
  readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  private id: number | null = null
  private closeRequested = false
  private closeSent = false

  constructor(path = '/api/ws') {
    const channel = new Channel<HostEvent>()
    channel.onmessage = event => this.receive(event)
    invoke<number>('desktop_ws_open', { path, onEvent: channel }).then(id => {
      this.id ??= id
      if (this.closeRequested) this.hostClose()
    }).catch(() => this.finish())
  }

  private receive(event: HostEvent) {
    if (this.readyState === 3) return
    this.id ??= event.id
    if (event.kind === 'open') {
      if (this.readyState === 0) { this.readyState = 1; this.onopen?.() }
    } else if (event.kind === 'message') {
      if (this.readyState === 1) this.onmessage?.({ data: event.data })
    } else {
      this.finish()
    }
  }

  private finish() {
    if (this.readyState === 3) return
    this.readyState = 3
    this.onclose?.()
  }

  private hostClose() {
    if (this.closeSent || this.id === null) return
    this.closeSent = true
    invoke('desktop_ws_close', { id: this.id }).catch(() => {})
  }

  send(data: string) {
    if (this.readyState === 0) throw new DOMException('The socket is still connecting', 'InvalidStateError')
    if (this.readyState !== 1 || this.id === null) return
    invoke('desktop_ws_send', { id: this.id, data }).catch(() => this.finish())
  }

  close() {
    if (this.readyState >= 2) return
    this.readyState = 2
    this.closeRequested = true
    this.hostClose()
  }
}
