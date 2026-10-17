import { HANDSHAKE_MAX_BYTES, HANDSHAKE_MAX_CHARS, mediaUserId } from './bounds'
import { clock } from './clock'
import { createMediaGuard, type MediaGuard } from './guard'
import { MediaGroup, type MediaStep } from './group'
import { WirePacer } from './pacer'
import type { MediaCall, MediaNotice } from './api'
import type { MediaVerification } from './verify'
import type { EpochKeys } from './frame'

// Nothing could be opened for this long while frames of a newer epoch keep
// arriving: the call is behind. A frame names its own epoch before it can be
// checked, so it takes this and the roster, never a frame alone.
const EVIDENCE_MS = 5000

const work = new WeakMap<MediaCall, Promise<void>>()

export function mediaSettled(call: MediaCall): Promise<void> {
  return work.get(call) ?? Promise.resolve()
}

function encodeWire(bytes: Uint8Array): string | null {
  if (bytes.byteLength === 0 || bytes.byteLength > HANDSHAKE_MAX_BYTES) return null
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  const text = btoa(binary)
  return text.length <= HANDSHAKE_MAX_CHARS ? text : null
}

function decodeWire(value: string): Uint8Array | null {
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

function rosterIds(ids: readonly string[], self: string | null): string[] {
  const members = new Set<string>()
  if (self) members.add(self)
  for (const value of ids) {
    const id = mediaUserId(value)
    if (id) members.add(id)
  }
  return [...members]
}

/** The call the browser actually runs. Tests install a double in front of it. */
export function browserMediaCall(guard: MediaGuard = createMediaGuard()): MediaCall {
  let group: MediaGroup | null = null
  let channelId: string | null = null
  let userId: string | null = null
  let roster: string[] = []
  let appliedEpoch: number | null = null
  let appliedKey: EpochKeys | null = null
  let appliedCaps: string | null = null
  let generation = 0
  let opening: Promise<void> = Promise.resolve()
  let tail: Promise<void> = Promise.resolve()
  const inbox: { from: string, wire: string }[] = []
  let keyListener: (() => void) | null = null
  let wireListener: ((channelId: string, wire: string) => void) | null = null
  let lastOpenedAt = clock.now()
  // When each publisher was first seen on a newer epoch, and last.
  const newerFrom = new Map<string, { first: number, last: number }>()
  let noticeListener: ((notice: MediaNotice) => void) | null = null
  const pacer = new WirePacer()
  let verificationListener: ((verification: MediaVerification | null) => void) | null = null
  let appliedCode: string | null = null

  const call: MediaCall = {
    start(nextChannel, nextUser) {
      const previous = group
      const channel = mediaUserId(nextChannel)
      const user = mediaUserId(nextUser)
      generation += 1
      const current = generation
      channelId = channel
      userId = user
      roster = user ? [user] : []
      appliedEpoch = null
      appliedKey = null
      inbox.length = 0
      resetEvidence()
      guard.reset()
      pacer.clear()
      verify(null)
      group = null
      if (previous) track(previous.close())
      if (!channel || !user) return
      opening = MediaGroup.open(user, channel).then(opened => {
        if (current !== generation) {
          void opened?.close()
          return
        }
        opened?.onNotice(notice => noticeListener?.(notice))
        opened?.onStep((step) => { if (current === generation) emit(step) })
        group = opened
      })
      track(flush(current))
    },
    roster(ids) {
      roster = rosterIds(ids, userId)
      group?.hint(roster)
      track(flush(generation))
    },
    receive(fromUserId, wire) {
      inbox.push({ from: fromUserId, wire })
      track(flush(generation))
    },
    resume() {
      track(flush(generation, true))
    },
    stop() {
      generation += 1
      const closing = group
      group = null
      channelId = null
      userId = null
      roster = []
      appliedEpoch = null
      appliedKey = null
      inbox.length = 0
      resetEvidence()
      guard.reset()
      pacer.clear()
      verify(null)
      track(closing ? closing.close() : Promise.resolve())
    },
    onKey(listener) { keyListener = listener },
    onWire(listener) { wireListener = listener },
    onNotice(listener) { noticeListener = listener },
    onVerification(listener) { verificationListener = listener },
    protectSender(sender, source, layer) {
      if (!channelId || !userId) return false
      return guard.armSender(sender, channelId, userId, source, layer)
    },
    prepareIncoming(receiver) {
      if (!receiver || !channelId) return false
      return guard.armReceiver(receiver, channelId, null)
    },
    protectIncoming(receiver, senderId, source) {
      const id = mediaUserId(senderId)
      if (!receiver || !channelId || !id) return false
      return guard.armReceiver(receiver, channelId, id, source)
    },
  }

  function resetEvidence() {
    lastOpenedAt = clock.now()
    newerFrom.clear()
  }

  async function recover(current: number): Promise<void> {
    await opening
    const live = group
    if (current !== generation || !live) return
    const step = await live.recover(roster)
    if (current === generation) emit(step)
  }

  guard.onStats((senders) => {
    const live = group
    if (!live) return
    const now = clock.now()
    for (const [sender, stats] of Object.entries(senders)) {
      if (stats.opened > 0) {
        lastOpenedAt = now
        live.opened()
      }
      if (stats.newer > 0 && stats.epoch > (appliedEpoch ?? -1)) {
        const before = newerFrom.get(sender)
        // A gap longer than the window starts the count again.
        newerFrom.set(sender, { first: before && now - before.last <= EVIDENCE_MS ? before.first : now, last: now })
      }
    }
    if (now - lastOpenedAt < EVIDENCE_MS) return
    // Sustained: seen for the whole window and still seen within it.
    const fresh = [...newerFrom].filter(([, seen]) => now - seen.last <= EVIDENCE_MS && now - seen.first >= EVIDENCE_MS).map(([id]) => id)
    // Two publishers agree, or the one who commits does.
    const committer = lowest([...roster, userId as string])
    if (fresh.length < 2 && !(committer && committer !== userId && fresh.includes(committer))) return
    // The group's own limit decides how often this may act.
    track(recover(generation))
  })

  function track(pending: Promise<void>) {
    const next = tail.then(() => pending)
    tail = next.then(() => undefined, () => undefined)
    work.set(call, tail)
  }

  function emit(step: MediaStep) {
    // A group exists only for a valid channel, and steps are dropped once the call moved on.
    const channel = channelId as string
    for (const wire of step.wires) {
      const text = encodeWire(wire)
      if (text) pacer.push(text, () => wireListener?.(channel, text))
    }
    if (step.key && step.epoch !== null) {
      if (step.epoch !== appliedEpoch || step.key !== appliedKey) {
        const replace = appliedEpoch === null || step.epoch <= appliedEpoch
        appliedEpoch = step.epoch
        appliedKey = step.key
        appliedCaps = capsId(step)
        guard.setKey(step.epoch, step.key, replace, step.soft === true, step.caps)
        keyListener?.()
      } else if (step.caps && capsId(step) !== appliedCaps) {
        // A member's advertisement arrived: the epoch is the same, the keys stay.
        appliedCaps = capsId(step)
        guard.setCaps(step.epoch, step.caps)
      }
      verify(step.verification ?? null)
      return
    }
    // Without a group (regrouping, or waiting to be let in again) the worker
    // keeps the last keys: it goes on opening with them, and sealing for a
    // short while, until the next group's key arrives as a new group.
    if (appliedEpoch !== null) guard.staleKeys()
    appliedEpoch = null
    appliedKey = null
    verify(null)
  }

  function capsId(step: MediaStep): string | null {
    return step.caps ? `${step.caps.complete}:${step.caps.members.join(',')}` : null
  }

  function verify(verification: MediaVerification | null) {
    const code = verification ? `${verification.epoch}:${verification.code}` : null
    if (code === appliedCode) return
    appliedCode = code
    verificationListener?.(verification)
  }

  async function flush(current: number, resend = false): Promise<void> {
    await opening
    const live = group
    if (current !== generation || !live) return
    // A step that finishes after the call moved on belongs to the old call.
    while (inbox.length && current === generation) {
      const item = inbox.shift()
      if (!item) break
      const raw = decodeWire(item.wire)
      if (!raw) continue
      const step = await live.receive(item.from, raw, true)
      if (current !== generation) return
      emit(step)
    }
    const step = await live.sync(roster, resend)
    if (current === generation) emit(step)
  }

  work.set(call, tail)
  return call
}
