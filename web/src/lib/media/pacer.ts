// The server relays at most HANDSHAKES_PER_SEC media handshakes and
// HANDSHAKE_BYTES_PER_SEC of them per connection and drops the rest without a
// word (internal/ws/budget.go). A dropped commit leaves every other member a
// step behind, so they start over. Sending below that limit costs a moment;
// a drop costs everyone a restart.
export const HANDSHAKES_PER_SEC = 8
export const HANDSHAKE_BYTES_PER_SEC = 192 << 10
// What the server counts besides the wire: the event envelope and channel id.
const ENVELOPE_BYTES = 128
const WINDOW_MS = 1000

/** Sends in order, holding back what would exceed the budget within any second. */
export class WirePacer {
  private readonly queue: { text: string, send: () => void }[] = []
  private readonly sent: { at: number, bytes: number }[] = []
  private timer: ReturnType<typeof setTimeout> | null = null

  constructor(private readonly now: () => number = Date.now) {}

  push(text: string, send: () => void) {
    this.queue.push({ text, send })
    this.drain()
  }

  /** Forgets what has not gone out yet: it belonged to a call that moved on. */
  clear() {
    this.queue.length = 0
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = null
  }

  private drain() {
    if (this.timer !== null) return
    while (this.queue.length) {
      const now = this.now()
      while (this.sent.length && now - (this.sent[0] as { at: number }).at >= WINDOW_MS) this.sent.shift()
      const next = this.queue[0] as { text: string, send: () => void }
      const bytes = next.text.length + ENVELOPE_BYTES
      const used = this.sent.reduce((sum, item) => sum + item.bytes, 0)
      // An empty window always lets one through, however large.
      if (this.sent.length && (this.sent.length >= HANDSHAKES_PER_SEC || used + bytes > HANDSHAKE_BYTES_PER_SEC)) {
        const wait = WINDOW_MS - (now - (this.sent[0] as { at: number }).at) + 1
        this.timer = setTimeout(() => {
          this.timer = null
          this.drain()
        }, wait)
        return
      }
      this.queue.shift()
      this.sent.push({ at: now, bytes })
      next.send()
    }
  }
}
