import { HANDSHAKE_MAX_BYTES, HANDSHAKE_MAX_CHARS, mediaUserId } from './bounds'
import { createMediaGuard, type MediaGuard } from './guard'
import { MediaGroup, type MediaStep } from './group'
import type { MediaCall, MediaNotice } from './api'

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
  let appliedKey: CryptoKey | null = null
  let generation = 0
  let opening: Promise<void> = Promise.resolve()
  let tail: Promise<void> = Promise.resolve()
  const inbox: { from: string, wire: string }[] = []
  let keyListener: (() => void) | null = null
  let wireListener: ((channelId: string, wire: string) => void) | null = null
  let lastOpenedAt = Date.now()
  const newerFrom = new Map<string, number>()
  let noticeListener: ((notice: MediaNotice) => void) | null = null

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
      group = null
      if (previous) track(previous.close())
      if (!channel || !user) return
      opening = MediaGroup.open(user, channel).then(opened => {
        if (current !== generation) {
          void opened?.close()
          return
        }
        opened?.onNotice(notice => noticeListener?.(notice))
        group = opened
      })
      track(flush(current))
    },
    roster(ids) {
      roster = rosterIds(ids, userId)
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
      track(closing ? closing.close() : Promise.resolve())
    },
    onKey(listener) { keyListener = listener },
    onWire(listener) { wireListener = listener },
    onNotice(listener) { noticeListener = listener },
    protectSender(sender) {
      if (!channelId || !userId) return false
      return guard.armSender(sender, channelId, userId)
    },
    prepareIncoming(receiver) {
      if (!receiver || !channelId) return false
      return guard.armReceiver(receiver, channelId, null)
    },
    protectIncoming(receiver, senderId) {
      const id = mediaUserId(senderId)
      if (!receiver || !channelId || !id) return false
      return guard.armReceiver(receiver, channelId, id)
    },
  }

  function resetEvidence() {
    lastOpenedAt = Date.now()
    newerFrom.clear()
  }

  async function recover(current: number): Promise<void> {
    await opening
    if (current !== generation || !group) return
    emit(await group.recover(roster))
  }

  guard.onStats((senders) => {
    const live = group
    if (!live) return
    const now = Date.now()
    for (const [sender, stats] of Object.entries(senders)) {
      if (stats.opened > 0) {
        lastOpenedAt = now
        live.opened()
      }
      if (stats.newer > 0 && stats.epoch > (appliedEpoch ?? -1)) newerFrom.set(sender, now)
    }
    if (now - lastOpenedAt < EVIDENCE_MS) return
    const fresh = [...newerFrom].filter(([, seen]) => now - seen <= EVIDENCE_MS).map(([id]) => id)
    // Two publishers agree, or the one who commits does.
    const committer = lowest(userId ? [...roster, userId] : roster)
    if (fresh.length < 2 && !(committer && committer !== userId && fresh.includes(committer))) return
    newerFrom.clear()
    lastOpenedAt = now
    track(recover(generation))
  })

  function track(pending: Promise<void>) {
    const next = tail.then(() => pending)
    tail = next.then(() => undefined, () => undefined)
    work.set(call, tail)
  }

  function emit(step: MediaStep) {
    if (!channelId) return
    for (const wire of step.wires) {
      const text = encodeWire(wire)
      if (text) wireListener?.(channelId, text)
    }
    if (step.key && step.epoch !== null) {
      if (step.epoch !== appliedEpoch || step.key !== appliedKey) {
        const replace = appliedEpoch === null || step.epoch <= appliedEpoch
        appliedEpoch = step.epoch
        appliedKey = step.key
        guard.setKey(step.epoch, step.key, replace)
        keyListener?.()
      }
      return
    }
    if (appliedEpoch !== null) {
      appliedEpoch = null
      appliedKey = null
      guard.clearKeys()
    }
  }

  async function flush(current: number, resend = false): Promise<void> {
    await opening
    if (current !== generation || !group) return
    while (inbox.length && current === generation && group) {
      const item = inbox.shift()
      if (!item) break
      const raw = decodeWire(item.wire)
      if (!raw) continue
      emit(await group.receive(item.from, raw))
    }
    if (current !== generation || !group) return
    emit(await group.sync(roster, resend))
  }

  work.set(call, tail)
  return call
}
