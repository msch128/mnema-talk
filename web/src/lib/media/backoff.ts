/**
 * Lets an action happen once per key, then again only after a wait that
 * doubles each time it is used, up to a ceiling. Used for everything that can
 * throw group state away or swap a member's leaf, so a flood of messages (or a
 * misbehaving member) cannot turn into a reset loop. Quiet for a while
 * restarts the wait from the base.
 */
export class Backoff {
  private readonly entries = new Map<string, { next: number, wait: number }>()

  constructor(
    private readonly baseMs: number,
    private readonly maxMs: number,
    private readonly clock: () => number = () => Date.now(),
  ) {}

  allow(key: string): boolean {
    const now = this.clock()
    const entry = this.entries.get(key)
    if (entry && now < entry.next) return false
    const quiet = !entry || now > entry.next + this.maxMs
    const wait = quiet ? this.baseMs : Math.min(entry.wait * 2, this.maxMs)
    this.entries.set(key, { next: now + wait, wait })
    return true
  }

  /** How long until `key` may act again; 0 when it may now. */
  waitMs(key: string): number {
    const entry = this.entries.get(key)
    return entry ? Math.max(0, entry.next - this.clock()) : 0
  }

  reset(key?: string) {
    if (key === undefined) this.entries.clear()
    else this.entries.delete(key)
  }
}
