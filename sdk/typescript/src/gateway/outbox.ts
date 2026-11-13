// Frames from the bot: at most 4096 bytes each and 120 a minute per
// connection. Heartbeats may use the whole budget; other frames leave a few
// slots free for them and wait in a queue when the budget is spent.
import { WindowLimiter } from '../rest/limits.ts'

export const MAX_FRAME_BYTES = 4096
const PER_MINUTE = 120
const RESERVED_FOR_HEARTBEATS = 5

export class FrameTooLargeError extends Error {
  override name = 'FrameTooLargeError'
}

export class Outbox {
  private limiter = new WindowLimiter(PER_MINUTE, 60_000)
  private queue: string[] = []
  private timer: ReturnType<typeof setTimeout> | undefined
  private write: ((text: string) => void) | undefined

  /** Starts a connection's budget; queued frames are dropped. */
  attach(write: (text: string) => void): void {
    this.reset()
    this.write = write
  }

  /** Stops sending; the queue is dropped and no timer stays. */
  reset(): void {
    clearTimeout(this.timer)
    this.timer = undefined
    this.queue = []
    this.write = undefined
    this.limiter = new WindowLimiter(PER_MINUTE, 60_000)
  }

  /** Encodes a frame and sends it now, or queues it while the budget is spent. */
  send(frame: { op: number; d: unknown }, priority = false): void {
    const text = JSON.stringify(frame)
    const bytes = Buffer.byteLength(text, 'utf8')
    if (bytes > MAX_FRAME_BYTES) throw new FrameTooLargeError(`gateway frame of ${bytes} bytes exceeds ${MAX_FRAME_BYTES}`)
    if (priority) {
      this.limiter.take(Date.now())
      this.write?.(text)
      return
    }
    this.queue.push(text)
    this.flush()
  }

  /** Frames waiting for budget. */
  get pending(): number {
    return this.queue.length
  }

  private flush(): void {
    while (this.queue.length > 0 && this.write) {
      const now = Date.now()
      const wait = this.limiter.delay(now, PER_MINUTE - RESERVED_FOR_HEARTBEATS)
      if (wait > 0) {
        if (!this.timer) {
          this.timer = setTimeout(() => {
            this.timer = undefined
            this.flush()
          }, wait)
        }
        return
      }
      this.limiter.take(now)
      this.write(this.queue.shift()!)
    }
  }
}
