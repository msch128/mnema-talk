// Client-side rate limit buckets: a sliding window of at most `limit` uses
// per `windowMs`, plus a pause a 429 imposed.

/** At most `limit` uses in any `windowMs`. */
export class WindowLimiter {
  private readonly stamps: number[] = []
  readonly limit: number
  readonly windowMs: number

  constructor(limit: number, windowMs: number) {
    this.limit = limit
    this.windowMs = windowMs
  }

  /** Milliseconds until a use is allowed (with at most `limit` in the window); 0 when it is now. */
  delay(now: number, limit = this.limit): number {
    while (this.stamps.length > 0 && this.stamps[0]! <= now - this.windowMs) this.stamps.shift()
    const n = this.stamps.length
    if (n < limit) return 0
    return this.stamps[n - limit]! + this.windowMs - now
  }

  take(now: number): void {
    this.stamps.push(now)
  }
}

/** One route's state: an optional local limit and the pause of its last 429. */
export class Bucket {
  pausedUntil = 0
  readonly limiter: WindowLimiter | undefined

  constructor(limiter?: WindowLimiter) {
    this.limiter = limiter
  }
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/**
 * The bucket of a route: method and path with ids replaced, except a channel
 * id, which keeps channels apart (Discord's "major parameter"). Posting a
 * message and uploading a file share the per-channel message bucket.
 */
export function bucketKey(method: string, path: string): string {
  const parts = path.split('?')[0]!.split('/')
  const post = method === 'POST' && parts[1] === 'channels' && (parts[3] === 'messages' || parts[3] === 'upload') && parts.length === 4
  if (post) return `post-message:${parts[2]}`
  if (parts[1] === 'interactions') return `${method} /interactions/:id/:token/${parts.slice(4).join('/')}`
  const out = parts.map((p, i) => (UUID.test(p) && !(i === 2 && parts[1] === 'channels') ? ':id' : p))
  // One bucket for all emoji of a message's reactions.
  const reactions = out.indexOf('reactions')
  return `${method} ${(reactions < 0 ? out : out.slice(0, reactions + 1)).join('/')}`
}

/** Whether the route counts toward the 5 messages per 5 seconds per channel. */
export function isMessagePost(key: string): boolean {
  return key.startsWith('post-message:')
}
