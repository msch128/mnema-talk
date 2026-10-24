import { HANDSHAKE_MAX_BYTES, HANDSHAKE_MAX_CHARS, mediaUserId } from './bounds'
import { clock } from './clock'
import type { MediaGuard, FrameStats } from './guard'
import { MediaGroup, type MediaStep } from './group'
import type { WirePacer } from './pacer'
import type { MediaCall, MediaNotice } from './api'
import type { MediaVerification } from './verify'
import type { EpochKeys } from './frame'

// Nothing could be opened for this long while frames of a newer epoch keep
// arriving: the call is behind. A frame names its own epoch before it can be
// checked, so it takes this and the roster, never a frame alone.
const EVIDENCE_MS = 5000

export const work = new WeakMap<MediaCall, Promise<void>>()

/** Everything one browser call keeps: its group, roster, applied keys and listeners. */
export interface CallState {
  readonly guard: MediaGuard
  readonly pacer: WirePacer
  call: MediaCall | null
  group: MediaGroup | null
  channelId: string | null
  userId: string | null
  roster: string[]
  appliedEpoch: number | null
  appliedKey: EpochKeys | null
  generation: number
  opening: Promise<void>
  tail: Promise<void>
  readonly inbox: { from: string, wire: string }[]
  keyListener: (() => void) | null
  wireListener: ((channelId: string, wire: string) => void) | null
  lastOpenedAt: number
  // When each publisher was first seen on a newer epoch, and last.
  readonly newerFrom: Map<string, { first: number, last: number }>
  noticeListener: ((notice: MediaNotice) => void) | null
  verificationListener: ((verification: MediaVerification | null) => void) | null
  appliedCode: string | null
}

export function newCallState(guard: MediaGuard, pacer: WirePacer): CallState {
  return {
    guard, pacer, call: null, group: null, channelId: null, userId: null, roster: [],
    appliedEpoch: null, appliedKey: null, generation: 0,
    opening: Promise.resolve(), tail: Promise.resolve(), inbox: [],
    keyListener: null, wireListener: null, lastOpenedAt: clock.now(), newerFrom: new Map(),
    noticeListener: null, verificationListener: null, appliedCode: null
  }
}

export function encodeWire(bytes: Uint8Array): string | null {
  if (bytes.byteLength === 0 || bytes.byteLength > HANDSHAKE_MAX_BYTES) return null
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  const text = btoa(binary)
  return text.length <= HANDSHAKE_MAX_CHARS ? text : null
}

export function decodeWire(value: string): Uint8Array | null {
  if (!value || value.length > HANDSHAKE_MAX_CHARS) return null
  try {
    const binary = atob(value)
    if (binary.length === 0 || binary.length > HANDSHAKE_MAX_BYTES) return null
    const out = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
    return out
  } catch {
    return null
  }
}

function lowest(ids: readonly string[]): string | null {
  let best: string | null = null
  for (const id of ids) if (best === null || id < best) best = id
  return best
}

export function rosterIds(ids: readonly string[], self: string | null): string[] {
  const members = new Set<string>()
  if (self) members.add(self)
  for (const value of ids) {
    const id = mediaUserId(value)
    if (id) members.add(id)
  }
  return [...members]
}

export function resetEvidence(s: CallState) {
  s.lastOpenedAt = clock.now()
  s.newerFrom.clear()
}

export function track(s: CallState, pending: Promise<void>) {
  const next = s.tail.then(() => pending)
  s.tail = next.then(() => undefined, () => undefined)
  if (s.call) work.set(s.call, s.tail)
}

export function verify(s: CallState, verification: MediaVerification | null) {
  const code = verification ? `${verification.epoch}:${verification.code}` : null
  if (code === s.appliedCode) return
  s.appliedCode = code
  s.verificationListener?.(verification)
}

async function recover(s: CallState, current: number): Promise<void> {
  await s.opening
  const live = s.group
  if (current !== s.generation || !live) return
  const step = await live.recover(s.roster)
  if (current === s.generation) emit(s, step)
}

/** Counts, per publisher, how long frames of a newer epoch have been arriving. */
function noteNewerEpochs(s: CallState, senders: Record<string, FrameStats>, live: MediaGroup, now: number) {
  for (const [sender, stats] of Object.entries(senders)) {
    if (stats.opened > 0) {
      s.lastOpenedAt = now
      live.opened()
    }
    if (stats.newer > 0 && stats.epoch > (s.appliedEpoch ?? -1)) {
      const before = s.newerFrom.get(sender)
      // A gap longer than the window starts the count again.
      s.newerFrom.set(sender, { first: before && now - before.last <= EVIDENCE_MS ? before.first : now, last: now })
    }
  }
}

function hasCommitterEvidence(committer: string | null, userId: string | null, fresh: string[]): boolean {
  if (!committer || committer === userId) return false
  return fresh.includes(committer)
}

function hasRecoveryConsensus(fresh: string[], committer: string | null, userId: string | null): boolean {
  if (fresh.length >= 2) return true
  return hasCommitterEvidence(committer, userId, fresh)
}

export function handleStats(s: CallState, senders: Record<string, FrameStats>) {
  const live = s.group
  if (!live) return
  if (Object.values(senders).some(stats => stats.legacy > 0)) s.noticeListener?.('outdated')
  const now = clock.now()
  noteNewerEpochs(s, senders, live, now)
  if (now - s.lastOpenedAt < EVIDENCE_MS) return
  // Sustained: seen for the whole window and still seen within it.
  const fresh = [...s.newerFrom].filter(([, seen]) => now - seen.last <= EVIDENCE_MS && now - seen.first >= EVIDENCE_MS).map(([id]) => id)
  // Two publishers agree, or the one who commits does.
  const committer = lowest([...s.roster, s.userId as string])
  if (!hasRecoveryConsensus(fresh, committer, s.userId)) return
  // The group's own limit decides how often this may act.
  track(s, recover(s, s.generation))
}

function pushPacedWires(s: CallState, wires: readonly Uint8Array[]) {
  const channel = s.channelId as string
  for (const wire of wires) {
    const text = encodeWire(wire)
    if (text) s.pacer.push(text, () => s.wireListener?.(channel, text))
  }
}

function applyStepKey(s: CallState, step: MediaStep & { key: EpochKeys; epoch: number }) {
  if (step.epoch !== s.appliedEpoch || step.key !== s.appliedKey) {
    const replace = s.appliedEpoch === null || step.epoch <= s.appliedEpoch
    s.appliedEpoch = step.epoch
    s.appliedKey = step.key
    s.guard.setKey(step.epoch, step.key, replace, step.soft === true)
    s.keyListener?.()
  }
  verify(s, step.verification ?? null)
}

function clearStepKeys(s: CallState) {
  // Without a group (regrouping, or waiting to be let in again) the worker
  // keeps the last keys: it goes on opening with them, and sealing for a
  // short while, until the next group's key arrives as a new group.
  if (s.appliedEpoch !== null) s.guard.staleKeys()
  s.appliedEpoch = null
  s.appliedKey = null
  verify(s, null)
}

export function emit(s: CallState, step: MediaStep) {
  // A group exists only for a valid channel, and steps are dropped once the call moved on.
  pushPacedWires(s, step.wires)
  if (step.key && step.epoch !== null) {
    applyStepKey(s, step as MediaStep & { key: EpochKeys; epoch: number })
    return
  }
  clearStepKeys(s)
}

async function drainInbox(s: CallState, live: MediaGroup, current: number): Promise<boolean> {
  // A step that finishes after the call moved on belongs to the old call.
  while (s.inbox.length && current === s.generation) {
    const item = s.inbox.shift()
    if (!item) break
    const raw = decodeWire(item.wire)
    if (!raw) continue
    const step = await live.receive(item.from, raw, true)
    if (current !== s.generation) return false
    emit(s, step)
  }
  return current === s.generation
}

export async function flush(s: CallState, current: number, resend = false): Promise<void> {
  await s.opening
  const live = s.group
  if (current !== s.generation || !live) return
  if (!(await drainInbox(s, live, current))) return
  const step = await live.sync(s.roster, resend)
  if (current === s.generation) emit(s, step)
}

/** Forgets the call's channel, keys and pending wires, ready for another channel or none. */
function clearCall(s: CallState) {
  s.appliedEpoch = null
  s.appliedKey = null
  s.inbox.length = 0
  resetEvidence(s)
  s.guard.reset()
  s.pacer.clear()
  verify(s, null)
}

export function startCall(s: CallState, nextChannel: string, nextUser: string) {
  const previous = s.group
  const channel = mediaUserId(nextChannel)
  const user = mediaUserId(nextUser)
  s.generation += 1
  const current = s.generation
  s.channelId = channel
  s.userId = user
  s.roster = user ? [user] : []
  clearCall(s)
  s.group = null
  if (previous) track(s, previous.close())
  if (!channel || !user) return
  s.opening = MediaGroup.open(user, channel).then(opened => {
    if (current !== s.generation) {
      void opened?.close()
      return
    }
    opened?.onNotice(notice => s.noticeListener?.(notice))
    opened?.onStep((step) => { if (current === s.generation) emit(s, step) })
    s.group = opened
  })
  track(s, flush(s, current))
}

export function stopCall(s: CallState) {
  s.generation += 1
  const closing = s.group
  s.group = null
  s.channelId = null
  s.userId = null
  s.roster = []
  clearCall(s)
  track(s, closing ? closing.close() : Promise.resolve())
}
