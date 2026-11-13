// Heartbeats at the interval HELLO gave, the first one after a random part of
// it (jitter), and the detection of a connection whose ACKs stopped.

export class Heartbeat {
  private timer: ReturnType<typeof setTimeout> | undefined
  private acked = true
  private readonly beat: () => void
  private readonly onZombie: () => void
  private readonly random: () => number

  /** `beat` sends op 1; `onZombie` runs when an ACK is missing at the next beat. */
  constructor(beat: () => void, onZombie: () => void, random: () => number = Math.random) {
    this.beat = beat
    this.onZombie = onZombie
    this.random = random
  }

  start(intervalMs: number): void {
    this.stop()
    this.acked = true
    const tick = () => {
      if (!this.acked) {
        this.stop()
        this.onZombie()
        return
      }
      this.now()
      this.timer = setTimeout(tick, intervalMs)
    }
    this.timer = setTimeout(tick, Math.floor(intervalMs * this.random()))
  }

  /** Sends a heartbeat at once (also when the server asks with op 1). */
  now(): void {
    this.acked = false
    this.beat()
  }

  ack(): void {
    this.acked = true
  }

  stop(): void {
    clearTimeout(this.timer)
    this.timer = undefined
  }
}
