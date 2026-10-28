import { clock } from './clock'

const REPLAY_WINDOW = 64n
const REPLAY_MASK = (1n << REPLAY_WINDOW) - 1n
// Streams one sender may open per epoch. Only a verified unit starts a stream,
// so only the sender itself can use them up; a new epoch starts afresh.
export const REPLAY_STREAMS = 256
// A stream that has been silent this long is retired when its epoch is out of
// room: only its stream id and highest counter are kept.
export const REPLAY_IDLE_MS = 60_000
// Retired streams kept per epoch. Past this a new stream is refused again, so
// memory stays bounded and no retired id is ever forgotten.
export const REPLAY_RETIRED = 1024

/**
 * One sender's units seen per (epoch, stream): the highest counter and a
 * 64-unit bitmap below it, as SRTP does. A unit older than the window, or one
 * already seen, is refused. A stream is never forgotten while its epoch is
 * kept: once a sender has REPLAY_STREAMS live streams in an epoch, those idle
 * for REPLAY_IDLE_MS are retired to their highest counter (a retired stream
 * reopens only for a counter above it, so nothing it opened can open again);
 * if none is idle, or REPLAY_RETIRED are retired already, further new streams
 * are refused until the next epoch.
 */
export class ReplayWindow {
  private readonly streams = new Map<string, { top: number, seen: bigint, at: number }>()
  private readonly retired = new Map<string, number>()

  constructor(private readonly now: () => number = () => clock.now()) {}

  fresh(epoch: number, stream: number, counter: number): boolean {
    const id = `${epoch}:${stream}`
    const entry = this.streams.get(id)
    if (!entry) {
      const high = this.retired.get(id)
      return (high === undefined || counter > high) && this.room(epoch)
    }
    if (counter > entry.top) return true
    const behind = BigInt(entry.top - counter)
    return behind < REPLAY_WINDOW && ((entry.seen >> behind) & 1n) === 0n
  }

  /** Records a unit. False when it was not fresh after all. */
  mark(epoch: number, stream: number, counter: number): boolean {
    if (!this.fresh(epoch, stream, counter)) return false
    const id = `${epoch}:${stream}`
    const entry = this.streams.get(id)
    const at = this.now()
    if (!entry) {
      // A reopened stream counts everything below its new top as seen: the
      // window of what it opened before was dropped when it was retired.
      const reopened = this.retired.delete(id)
      this.streams.set(id, { top: counter, seen: reopened ? REPLAY_MASK : 1n, at })
    } else if (counter > entry.top) {
      const ahead = BigInt(counter - entry.top)
      const seen = ahead >= REPLAY_WINDOW ? 1n : ((entry.seen << ahead) | 1n) & REPLAY_MASK
      this.streams.set(id, { top: counter, seen, at })
    } else {
      this.streams.set(id, { top: entry.top, seen: entry.seen | (1n << BigInt(entry.top - counter)), at })
    }
    return true
  }

  /** Whether a stream this epoch has not seen may open; retires idle ones to make room. */
  private room(epoch: number): boolean {
    const prefix = `${epoch}:`
    const mine = [...this.streams.keys()].filter(id => id.startsWith(prefix))
    if (mine.length < REPLAY_STREAMS) return true
    const now = this.now()
    let retired = [...this.retired.keys()].filter(id => id.startsWith(prefix)).length
    let freed = false
    for (const id of mine) {
      const entry = this.streams.get(id) as { top: number, at: number }
      if (now - entry.at <= REPLAY_IDLE_MS || retired >= REPLAY_RETIRED) continue
      this.retired.set(id, entry.top)
      this.streams.delete(id)
      retired++
      freed = true
    }
    return freed
  }

  /** Forgets streams of epochs whose keys are gone. */
  retain(epochs: Iterable<number>) {
    const kept = new Set([...epochs].map(String))
    for (const id of [...this.streams.keys()]) if (!kept.has(id.slice(0, id.indexOf(':')))) this.streams.delete(id)
    for (const id of [...this.retired.keys()]) if (!kept.has(id.slice(0, id.indexOf(':')))) this.retired.delete(id)
  }
}
