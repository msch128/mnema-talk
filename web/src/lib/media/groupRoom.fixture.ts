// A Talk on production timing for tests: real MediaGroups, a relay that hands
// every wire to every other member (as the server does) and a fake clock the
// groups' own timers run on. Unlike the simulator in groupSim.fixture.ts it
// interferes with nothing; it is for watching a room over an hour.
import { vi } from 'vitest'
import * as mls from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import type { MediaTiming } from './bounds'
import { MediaGroup, type MediaStep } from './group'

export const ROOM_IDS = Array.from({ length: 13 }, (_, index) => fixtureId(index + 1))
export const ROOM_CHANNEL = fixtureId(9999)
export const ROOM_START = Date.UTC(2026, 0, 1)
/** The product's timing, without the jitter. */
export const PRODUCTION: MediaTiming = { rotateMs: 30 * 60_000, rotateJitterMs: 0, absentMs: 20 * 60_000, heartbeatMs: 5 * 60_000, maintenanceMs: 30_000 }
const FAKED: ('Date' | 'setTimeout' | 'clearTimeout')[] = ['Date', 'setTimeout', 'clearTimeout']
const EMPTY = new Uint8Array(0)
/** The call starts over when frames of a newer epoch kept arriving for this long (callEngine.ts). */
const BEHIND_MS = 5000

export type WireKind = 'heartbeat' | 'capability' | 'key-package' | 'welcome' | 'commit' | 'other'

export function wireKind(wire: Uint8Array): WireKind {
  if (wire[0] === 0x4d && wire[1] === 0x48) return 'heartbeat'
  if (wire[0] === 0x4d && wire[1] === 0x43) return 'capability'
  const message = mls.decodeMlsMessage(wire, 0)?.[0]
  if (message?.wireformat === 'mls_key_package') return 'key-package'
  if (message?.wireformat === 'mls_welcome') return 'welcome'
  return message?.wireformat === 'mls_public_message' && message.publicMessage.content.contentType === 'commit' ? 'commit' : 'other'
}

function view(step: MediaStep): string {
  return step.verification ? `${step.epoch}:${step.verification.code}` : 'none'
}

export class TalkRoom {
  readonly groups = new Map<string, MediaGroup>()
  /** Members in different parts do not hear each other: two halves of a Talk that formed apart. */
  readonly part = new Map<string, number>()
  /** Wires sent since the last clear, by kind. */
  readonly sent = new Map<WireKind, number>()
  private readonly rosters = new Map<string, string[]>()
  private readonly views = new Map<string, string>()
  private readonly behindSince = new Map<string, number>()
  private queue: { from: string, wire: Uint8Array }[] = []
  /** Called with every wire a member sends. */
  onWire: (from: string, kind: WireKind, wire: Uint8Array) => void = () => {}

  /**
   * With `engine`, a member that holds an older epoch than someone else in the
   * room for five seconds starts over, as the call does when frames of a newer
   * epoch keep arriving. Without it nobody sends media, and only the groups act.
   */
  constructor(private readonly engine = true, private readonly timing: MediaTiming = PRODUCTION) {
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    vi.useFakeTimers({ toFake: FAKED, now: ROOM_START })
  }

  count(kind: WireKind): number {
    return this.sent.get(kind) ?? 0
  }

  private take(id: string, step: MediaStep) {
    for (const wire of step.wires) {
      const kind = wireKind(wire)
      this.sent.set(kind, this.count(kind) + 1)
      this.onWire(id, kind, wire)
      this.queue.push({ from: id, wire })
    }
    this.views.set(id, view(step))
  }

  async join(id: string, roster: string[], part = 0): Promise<MediaGroup> {
    const group = await MediaGroup.open(id, ROOM_CHANNEL, this.timing)
    if (!group) throw new Error('expected a media group')
    group.onStep(step => this.take(id, step))
    this.groups.set(id, group)
    this.part.set(id, part)
    await this.list(roster, roster)
    return group
  }

  /** Members join one after the other, each seeing those before it, a few seconds apart. */
  async form(ids: string[], part = 0) {
    for (const [index, id] of ids.entries()) {
      await this.join(id, ids.slice(0, index + 1), part)
      await this.run(3000)
    }
  }

  /** The tab stops for good; the Talk goes on listing it. */
  async hang(id: string) {
    await this.groups.get(id)?.close()
    this.groups.delete(id)
    this.views.delete(id)
  }

  /** The server tells `only` (or everyone) who is in the Talk. */
  async list(roster: string[], only?: string[]) {
    for (const [id, group] of this.groups) {
      if (only && !only.includes(id)) continue
      this.rosters.set(id, roster)
      this.take(id, await group.sync(roster))
    }
    await this.settle()
  }

  /** A wire under `from`'s id reaches everyone in its part, or `to`, whoever made it and whenever. */
  async inject(from: string, wire: Uint8Array, to?: string[]) {
    if (!to) {
      this.queue.push({ from, wire })
      return this.settle()
    }
    for (const id of to) {
      const group = this.groups.get(id) as MediaGroup
      this.take(id, await group.receive(from, wire.slice(), true))
      this.take(id, await group.sync(this.rosters.get(id) as string[]))
    }
    await this.settle()
  }

  private async drain() {
    for (const group of this.groups.values()) await group.receive('', EMPTY)
  }

  async settle() {
    for (let round = 0; round < 50; round++) {
      await this.drain()
      if (!this.queue.length) await this.drain()
      if (!this.queue.length) return
      const batch = this.queue
      this.queue = []
      for (const item of batch) {
        for (const [id, group] of this.groups) {
          if (id === item.from || this.part.get(id) !== (this.part.get(item.from) ?? 0)) continue
          // Each browser decodes its own bytes; the call syncs to the roster after every handshake.
          this.take(id, await group.receive(item.from, item.wire.slice(), true))
          this.take(id, await group.sync(this.rosters.get(id) as string[]))
        }
      }
    }
    throw new Error('the room did not settle')
  }

  /** Lets `ms` pass in steps of a second; returns early, with true, once `until` holds. */
  async run(ms: number, until?: () => boolean): Promise<boolean> {
    for (let done = 0; done < ms; done += 1000) {
      vi.advanceTimersByTime(1000)
      await this.settle()
      if (this.engine) await this.startOverBehind()
      if (until?.()) return true
    }
    return false
  }

  private async startOverBehind() {
    const epochs = new Map([...this.groups.keys()].map(id => [id, Number.parseInt(this.views.get(id) ?? '', 10)]))
    const newest = Math.max(...[...epochs.values()].filter(epoch => !Number.isNaN(epoch)))
    for (const [id, epoch] of epochs) {
      if (Number.isNaN(epoch) || epoch >= newest) {
        this.behindSince.delete(id)
        continue
      }
      const since = this.behindSince.get(id) ?? Date.now()
      this.behindSince.set(id, since)
      if (Date.now() - since < BEHIND_MS) continue
      this.behindSince.delete(id)
      this.take(id, await (this.groups.get(id) as MediaGroup).recover(this.rosters.get(id) as string[]))
      await this.settle()
    }
  }

  /** Everyone present holds the same epoch and key. */
  agreed(): boolean {
    const all = [...this.groups.keys()].map(id => this.views.get(id))
    return all.every(held => held !== undefined && held !== 'none' && held === all[0])
  }

  /** Runs for `ms` and returns for how many of its seconds the members present did not share one key. */
  async secondsApart(ms: number): Promise<number> {
    let apart = 0
    for (let done = 0; done < ms; done += 1000) {
      await this.run(1000)
      if (!this.agreed()) apart++
    }
    return apart
  }

  async close() {
    for (const group of this.groups.values()) await group.close()
    vi.useRealTimers()
    vi.restoreAllMocks()
  }
}
