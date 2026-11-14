// A lower bound of the server's clock, for ending messages with a lifetime
// on this client (doc/timed-messages.md). Every server timestamp the client
// sees (a message's created_at, the time of voice_rooms) is a moment the
// server had passed when the client read it, however late it arrived: a
// delayed answer only makes the estimate earlier, never later. The estimate is
// the latest of those moments plus the time elapsed since each was read,
// measured on both the wall clock and the monotonic clock, whichever is
// shorter: a wall clock that is stepped forward cannot move it ahead. So a
// message is never ended before its server-side end; at worst a little after.

interface Sample { server: number; wall: number; mono: number }

const MAX_SAMPLES = 32
let samples: Sample[] = []

const mono = () => (typeof performance !== 'undefined' ? performance.now() : Date.now())

/** Notes a server timestamp (ISO or ms) read just now. */
export function noteServerTime(at: string | number | null | undefined): void {
  const server = typeof at === 'number' ? at : Date.parse(at ?? '')
  if (!Number.isFinite(server)) return
  samples.push({ server, wall: Date.now(), mono: mono() })
  if (samples.length > MAX_SAMPLES) {
    // Keep the most useful: the latest estimates now.
    const now = { wall: Date.now(), mono: mono() }
    samples = samples.sort((a, b) => estimate(b, now) - estimate(a, now)).slice(0, MAX_SAMPLES / 2)
  }
}

function estimate(s: Sample, now: { wall: number; mono: number }): number {
  const elapsed = Math.max(0, Math.min(now.wall - s.wall, now.mono - s.mono))
  return s.server + elapsed
}

/** The server's time now, never later than the truth; null while no timestamp was seen. */
export function serverNowAtLeast(): number | null {
  if (!samples.length) return null
  const now = { wall: Date.now(), mono: mono() }
  let best = -Infinity
  for (const s of samples) best = Math.max(best, estimate(s, now))
  return best
}

/** Forgets every sample (a new community, tests). */
export function resetServerTime(): void {
  samples = []
}
