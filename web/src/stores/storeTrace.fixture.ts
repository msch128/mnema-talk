// Replays a store trace (internal/server/store_trace_integration_test.go) into
// the real stores: a fake socket delivers the recorded frames, a fake fetch
// answers every request from the server snapshot of the right moment, and a
// delivery plan perturbs the order (a dropped connection, an answer that comes
// early or late, a repeated frame). The canonical state of the stores is then
// compared with that of a client that has just loaded the same snapshot.
import { serverOffset } from '../shared/lib/clock'
import { noteServerTime, resetServerTime } from '../shared/lib/serverTime'
import { vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { useChatStore } from './chat'
import { useLaterStore } from './later'
import { useSavedStore } from './saved'
import { userFixture } from '../test-fixtures.fixture'

export interface TraceFrame { type: string; payload: unknown }
export interface TraceSnapshot { op: number; frames: number; bodies: Record<string, unknown> }
export interface StoreTrace {
  seed: number
  /** When the trace was made; messages with a lifetime end by the server clock of that moment. */
  recorded_at?: string
  me: { id: string; username: string }
  snapshots: TraceSnapshot[]
  frames: TraceFrame[]
  ops: Record<string, number>
  log: string[]
}

/**
 * How one run departs from the recorded order. `drop` loses the frames from
 * `at` up to the snapshot `until` (the connection was down meanwhile) and
 * reconnects; with `answer` the requests of the reconnect are answered early or
 * late as below. `repeat` delivers frame `at` twice. `early` answers the
 * requests made at frame `at` from a state that already holds the next frame's
 * change, before that frame arrives; `late` answers them from the state of
 * frame `at`, after `by` more frames have arrived.
 */
export type Plan =
  | { kind: 'clean' }
  | { kind: 'drop'; at: number; until: number; answer?: 'early' | 'late' }
  | { kind: 'repeat'; at: number }
  | { kind: 'early'; at: number }
  | { kind: 'late'; at: number; by: number }

export interface Canonical {
  unread: Record<string, [number, number]>
  followed: string[]
  reminders: string[]
  scheduled: string[]
  saved: string[]
}

class FakeSocket {
  static instances: FakeSocket[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn<(data: string) => void>()
  close = vi.fn()
  constructor() { FakeSocket.instances.push(this) }
}

interface Waiting { path: string; snapshot: number; release: number; resolve: (r: Response) => void }
type Answer = { snapshot: number; release: number }

/** The server as the replayed client sees it: the snapshots, and requests waiting for their answer. */
class TraceServer {
  delivered = 0
  waiting: Waiting[] = []
  override: (() => Answer) | null = null
  private readonly full: Record<string, unknown>[]

  constructor(readonly trace: StoreTrace) {
    let carried: Record<string, unknown> = {}
    this.full = trace.snapshots.map(s => (carried = { ...carried, ...s.bodies }))
  }

  /** The first state that holds every operation whose frames were delivered; its answer waits for that operation's frames. */
  consistent(delivered: number): number {
    const i = this.trace.snapshots.findIndex(s => s.frames >= delivered)
    return i < 0 ? this.trace.snapshots.length - 1 : i
  }

  fetch(input: unknown): Promise<Response> {
    const path = String(input).split('?')[0] ?? ''
    if (!(path in (this.full[0] ?? {}))) {
      return Promise.resolve(new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'not in trace' } }), { status: 404 }))
    }
    const snapshot = this.override?.().snapshot ?? this.consistent(this.delivered)
    const release = this.override?.().release ?? this.trace.snapshots[snapshot]?.frames ?? 0
    return new Promise(resolve => { this.waiting.push({ path, snapshot, release, resolve }); this.flush() })
  }

  flush() {
    const ready = this.waiting.filter(w => w.release <= this.delivered)
    this.waiting = this.waiting.filter(w => w.release > this.delivered)
    for (const w of ready) w.resolve(new Response(JSON.stringify(this.full[w.snapshot]?.[w.path]), { status: 200 }))
  }

  /** The threads the server says the member follows in a snapshot. */
  following(snapshot: number): Set<string> {
    const body = this.full[snapshot]?.['/api/threads/following'] as { thread_ids?: string[] } | undefined
    return new Set(body?.thread_ids ?? [])
  }

  /** Answers whatever still waits (the end of the trace: everything has arrived). */
  drain() {
    for (const w of this.waiting) w.release = 0
    this.flush()
  }

  /**
   * The next requests are answered out of line, as if made while frame `at` is
   * handled: `early` from the state that holds the next frame too, at once (the
   * server read it after that operation committed; its frame is still on the
   * way); `late` from the state that holds frame `at`, after `by` more frames
   * (frames sent after the server read the answer overtake it).
   */
  arm(kind: 'early' | 'late', at: number, by = 2) {
    const answer = kind === 'early'
      ? { snapshot: this.consistent(at + 2), release: 0 }
      : { snapshot: this.consistent(at + 1), release: at + 1 + by }
    this.override = () => answer
  }
}

const settle = async () => { for (let i = 0; i < 6; i++) await new Promise(r => setImmediate(r)) }

function canonical(): Canonical {
  const chat = useChatStore()
  const later = useLaterStore()
  const unread: Canonical['unread'] = {}
  for (const [id, s] of Object.entries(chat.readStates)) {
    if (s.unread_count || s.mention_count) unread[id] = [s.unread_count, s.mention_count]
  }
  return {
    unread,
    followed: [...chat.followedThreads].sort(),
    reminders: later.reminders.map(r => `${r.id}:${r.delivered_at === null ? 'pending' : 'due'}`).sort(),
    scheduled: later.scheduled.map(s => `${s.id}:${s.state}`).sort(),
    saved: useSavedStore().items.map(e => e.message_id).sort()
  }
}

type Chat = ReturnType<typeof useChatStore>

function install(server: TraceServer): Chat {
  setActivePinia(createPinia())
  // The client's server clock is the recording's, so a lifetime does not end on the replay's own clock.
  const recorded = server.trace.recorded_at ? Date.parse(server.trace.recorded_at) : NaN
  serverOffset.value = Number.isFinite(recorded) ? recorded - Date.now() : 0
  resetServerTime()
  if (Number.isFinite(recorded)) noteServerTime(recorded)
  FakeSocket.instances = []
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.stubGlobal('fetch', vi.fn((input: unknown) => server.fetch(input)))
  const auth = useAuthStore()
  auth.user = userFixture({ id: server.trace.me.id, username: server.trace.me.username })
  vi.spyOn(auth, 'checkAuth').mockResolvedValue(true)
  return useChatStore()
}

async function open(chat: Chat, first: boolean) {
  if (first) chat.initWebSocket()
  else chat.retryNow()
  await settle()
  const socket = FakeSocket.instances.at(-1)
  socket?.onopen?.()
  await settle()
  return socket
}

/** What the header's saved and later panel loads when it mounts (SavedLaterPanel.vue). */
function mountPanel() {
  void useSavedStore().ensureLoaded()
  void useLaterStore().load({ reminders: true, scheduled: true })
}

/** Delivers a trace's frames to one replayed client under a plan. */
class Replayer {
  socket: FakeSocket | undefined
  constructor(readonly trace: StoreTrace, readonly plan: Plan, readonly server: TraceServer, readonly chat: Chat) {}

  /** The connection drops before frame `at`, the frames up to snapshot `until` are lost, the client reconnects. Returns the next frame. */
  async drop(plan: Extract<Plan, { kind: 'drop' }>): Promise<number> {
    this.socket?.onclose?.()
    await settle()
    const server = this.server
    server.delivered = Math.max(plan.at, this.trace.snapshots[plan.until]?.frames ?? this.trace.frames.length)
    if (plan.answer) server.arm(plan.answer, server.delivered - 1)
    this.socket = await open(this.chat, false)
    server.override = null
    return server.delivered
  }

  /**
   * A reply by someone else that does not name the member notifies it only if
   * it follows the thread (threadAllows, notifications.ts). The client decides
   * from its list as it is when the reply arrives; the server's list for that
   * reply is the one stored with it. Returns the thread when the two differ.
   */
  replyDisagrees(i: number): string | null {
    const frame = this.trace.frames[i]
    if (frame?.type !== 'message_create') return null
    const m = frame.payload as { user_id: string; parent_id?: string | null; mentions?: string[]; reply_to?: { user_id?: string } | null }
    const me = this.trace.me.id
    if (!m.parent_id || m.user_id === me || m.mentions?.includes(me) || m.reply_to?.user_id === me) return null
    const server = this.server.following(this.server.consistent(i + 1)).has(m.parent_id)
    return this.chat.followedThreads.has(m.parent_id) === server ? null : m.parent_id
  }

  async deliver(i: number) {
    const plan = this.plan
    const out = (plan.kind === 'early' || plan.kind === 'late') && i === plan.at
    if (out) this.server.arm(plan.kind, i, plan.kind === 'late' ? plan.by : 0)
    const data = JSON.stringify(this.trace.frames[i])
    // Whatever the frame makes the client ask is answered from a state that holds the frame's change.
    this.server.delivered = i + 1
    this.socket?.onmessage?.({ data })
    if (plan.kind === 'repeat' && i === plan.at) this.socket?.onmessage?.({ data })
    this.server.flush()
    await settle()
    if (out) this.server.override = null
  }
}

/** The last snapshot read when exactly `frames` frames had arrived, or -1. */
export function lastSnapshotAt(trace: StoreTrace, frames: number): number {
  const s = trace.snapshots
  for (let j = s.length - 1; j >= 0; j--) if (s[j]?.frames === frames) return j
  return -1
}

/**
 * The open client after the run: every frame delivered under the plan, the
 * panel's lists loaded once at start. `checkpoint` (optional) sees the
 * client's state whenever the frames of a snapshot have all arrived and no
 * answer is outstanding.
 */
export interface Hooks {
  checkpoint?: (snapshot: number, state: Canonical) => void
  /** A reply whose notification the client decides from a follow list that differs from the server's. */
  reply?: (frame: number, thread: string) => void
}

export async function replay(trace: StoreTrace, plan: Plan, hooks: Hooks = {}): Promise<Canonical> {
  const { checkpoint } = hooks
  const server = new TraceServer(trace)
  const r = new Replayer(trace, plan, server, install(server))
  r.socket = await open(r.chat, true)
  mountPanel()
  await settle()
  for (let i = 0; i < trace.frames.length; i++) {
    if (plan.kind === 'drop' && i === plan.at) i = await r.drop(plan)
    if (i >= trace.frames.length) break
    const thread = hooks.reply ? r.replyDisagrees(i) : null
    if (thread) hooks.reply?.(i, thread)
    await r.deliver(i)
    const j = lastSnapshotAt(trace, i + 1)
    if (checkpoint && j >= 0 && server.waiting.length === 0) checkpoint(j, canonical())
  }
  server.drain()
  await settle()
  const state = canonical()
  r.chat.closeWebSocket()
  return state
}

/** A client that has just loaded the final snapshot (or the one at index `at`). */
export async function fresh(trace: StoreTrace, at = trace.snapshots.length - 1): Promise<Canonical> {
  const server = new TraceServer(trace)
  server.override = () => ({ snapshot: at, release: 0 })
  const chat = install(server)
  await open(chat, true)
  mountPanel()
  await settle()
  const state = canonical()
  chat.closeWebSocket()
  return state
}

const LISTS = ['followed', 'reminders', 'scheduled', 'saved'] as const

/** Every compared value by name: `unread:<channel>`, `mentions:<channel>`, and the lists. */
function fields(c: Canonical): Map<string, string> {
  const out = new Map<string, string>()
  for (const [id, [unread, mentions]] of Object.entries(c.unread)) {
    out.set(`unread:${id}`, String(unread))
    out.set(`mentions:${id}`, String(mentions))
  }
  for (const key of LISTS) out.set(key, c[key].join(','))
  return out
}

/** The values that differ from `want` (a counter missing on one side is zero). */
export function wrong(got: Canonical, want: Canonical): Set<string> {
  const g = fields(got), w = fields(want)
  const out = new Set<string>()
  for (const key of new Set([...g.keys(), ...w.keys()])) {
    if ((g.get(key) ?? '0') !== (w.get(key) ?? '0')) out.add(key)
  }
  return out
}

/** The class of a value's name: what kind of state went wrong, without the channel. */
export const classOf = (key: string) => key.split(':')[0] ?? key

/**
 * Threads whose root was deleted in the trace: the server forgets the follow,
 * the client keeps an id no reply can carry any more. Nothing shows it, so the
 * comparison leaves those ids out.
 */
export function withoutDeletedThreads(c: Canonical, trace: StoreTrace, upTo = trace.frames.length): Canonical {
  const gone = new Set(trace.frames.slice(0, upTo).filter(f => f.type === 'message_delete').map(f => (f.payload as { id: string }).id))
  return { ...c, followed: c.followed.filter(id => !gone.has(id)) }
}

/** A short description of where two canonical states differ. */
export function difference(got: Canonical, want: Canonical): string[] {
  const out: string[] = []
  const ids = new Set([...Object.keys(got.unread), ...Object.keys(want.unread)])
  for (const id of ids) {
    const [gu, gm] = got.unread[id] ?? [0, 0]
    const [wu, wm] = want.unread[id] ?? [0, 0]
    if (gu !== wu) out.push(`unread ${id.slice(0, 8)}: ${gu}, fresh ${wu}`)
    if (gm !== wm) out.push(`mentions ${id.slice(0, 8)}: ${gm}, fresh ${wm}`)
  }
  for (const key of LISTS) {
    const g = got[key].join(','), w = want[key].join(',')
    if (g !== w) out.push(`${key}: [${g}], fresh [${w}]`)
  }
  return out
}
