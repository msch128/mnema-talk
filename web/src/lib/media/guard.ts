import type { EpochKeys, FrameSource } from './frame'

export interface FrameStats { opened: number, newer: number, epoch: number, legacy: number }

interface KeyedPort {
  postMessage(data: unknown): void
  close(): void
}

export interface MediaGuard {
  armSender(sender: RTCRtpSender, channelId: string, senderId: string, source: FrameSource, layer?: number, dtx?: boolean): boolean
  /**
   * Arms a receiver. With no sender yet it opens nothing until bindReceiver
   * names the publisher. Arming an armed receiver only names the publisher
   * and what its line carries now.
   */
  armReceiver(receiver: RTCRtpReceiver, channelId: string, senderId: string | null, source?: FrameSource): boolean
  /**
   * With soft, the epoch was reached by a commit that removed nobody: sealing may stay one moment on the epoch left.
   */
  setKey(epoch: number, key: EpochKeys, replace?: boolean, soft?: boolean): void
  /** The call lost its group: the last keys may seal only a little longer. */
  staleKeys(): void
  /** Per publisher: frames opened, frames from an epoch newer than any key held, and units in the retired version 2 format. */
  onStats(listener: (senders: Record<string, FrameStats>) => void): void
  reset(): void
}

interface TransformTarget {
  transform: RTCRtpScriptTransform | null
}

function count(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0
}

function isStatsMessage(data: unknown): data is { type: 'stats', senders: unknown } {
  if (!data || typeof data !== 'object') return false
  if (!('type' in data) || data.type !== 'stats') return false
  return 'senders' in data
}

function isStatsEntry(entry: unknown): entry is { opened: unknown, newer: unknown, epoch: unknown, legacy: unknown } {
  if (!entry || typeof entry !== 'object') return false
  if (!('opened' in entry) || !('newer' in entry)) return false
  return 'epoch' in entry && 'legacy' in entry
}

function statsOf(entry: { opened: unknown, newer: unknown, epoch: unknown, legacy: unknown }): FrameStats | null {
  const { opened, newer, epoch, legacy } = entry
  if (!count(opened) || !count(newer) || !count(legacy)) return null
  if (typeof epoch !== 'number' || !Number.isInteger(epoch)) return null
  return { opened, newer, epoch, legacy }
}

function readStats(data: unknown): Record<string, FrameStats> | null {
  if (!isStatsMessage(data)) return null
  const raw = data.senders
  if (!raw || typeof raw !== 'object') return null
  const out: Record<string, FrameStats> = {}
  for (const [id, entry] of Object.entries(raw)) {
    if (!isStatsEntry(entry)) continue
    const stats = statsOf(entry)
    if (!stats) continue
    out[id] = stats
  }
  return out
}

export function canArm(): boolean {
  return typeof Worker === 'function' && typeof RTCRtpScriptTransform === 'function' && typeof MessageChannel === 'function'
}

type KeyMessage = { type: 'key', epoch: number, key: EpochKeys, replace?: true, soft?: true, seq: number }

interface KeyFields {
  epoch: number
  key: EpochKeys
  seq: number
  replace: boolean
  soft: boolean
}

function keyMessage(fields: KeyFields): KeyMessage {
  const { epoch, key, seq, replace, soft } = fields
  return {
    type: 'key' as const, epoch, key, seq,
    ...(replace ? { replace: true as const } : {}),
    ...(soft && !replace ? { soft: true as const } : {}),
  }
}

/** What a line's transform worker is told about it when it is armed. */
function transformOptions(port: MessagePort, direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource, layer?: number, dtx?: boolean) {
  return {
    port,
    direction,
    channelId,
    ...(senderId === null ? {} : { senderId }),
    ...(source === undefined ? {} : { source }),
    ...(layer === undefined ? {} : { layer }),
    ...(dtx ? { dtx: true } : {}),
  }
}

function spawnWorker(onStats: (senders: Record<string, FrameStats>) => void): Worker {
  const worker = new Worker(new URL('./mediaFrameWorker.ts', import.meta.url), { type: 'module' })
  worker.onmessage = (event: MessageEvent<unknown>) => {
    const senders = readStats(event.data)
    if (senders) onStats(senders)
  }
  return worker
}

/** A port the browser already closed rejects; the call is gone either way. */
function ignoreClosedPort(error: unknown) {
  void error
}

function closeQuietly(port: KeyedPort) {
  try { port.close() } catch (error) { ignoreClosedPort(error) }
}

/**
 * Posts to a line's port. WebKit sometimes refuses to clone the CryptoKeys of a
 * key message ("The object can not be cloned", seen in about one macOS WebKit
 * call in two, on any page), and the next attempt goes through; so a refused
 * post is tried once more before it is given up. The call's one worker also
 * has the key from any other line that delivered it (seq), so a line that
 * stays refused is keyless only until the next key.
 */
function postOrWarn(port: KeyedPort, message: unknown) {
  for (let attempt = 1; ; attempt++) {
    try {
      port.postMessage(message)
      return
    } catch (error) {
      if (attempt < POST_ATTEMPTS) continue
      console.warn('[media] Could not hand the key to a line:', error)
      return
    }
  }
}

const POST_ATTEMPTS = 2

function isValidArmTarget(direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource): boolean {
  if (!channelId || senderId === '') return false
  if (direction === 'send') {
    return senderId !== null && source !== undefined
  }
  return true
}

// 'drop' empties the worker for the whole call, so only a reset sends it; a
// replaced line just closes its port and leaves the other lines' keys alone.
function releasePort(ports: Set<KeyedPort>, lines: Map<object, KeyedPort>, port: KeyedPort, dropKeys = false) {
  ports.delete(port)
  for (const [transform, line] of lines) if (line === port) lines.delete(transform)
  if (dropKeys) try { port.postMessage({ type: 'drop' }) } catch (error) { ignoreClosedPort(error) }
  try { port.close() } catch (error) { ignoreClosedPort(error) }
}

// A WebKit receiver object is not the same object every time it is asked for
// (an unreferenced one is collected and made anew), so the receiver alone
// forgot lines that were armed and armed them again: a second transform on a
// line already receiving, which delivered a second or two of frames and then
// none (the member could not be heard for the rest of the call). Receiving
// lines are therefore also found through the transform they were armed with,
// held strongly so that the object the receiver reports stays the same one.

/** One worker seals every local track and opens every remote track of this call. */
export function createMediaGuard(): MediaGuard {
  let worker: Worker | null = null
  const ports = new Set<KeyedPort>()
  // The port of the line each sender or receiver carries now, so arming it
  // again (renegotiation, a replaced track) releases the port it replaces.
  let armed = new WeakMap<TransformTarget, KeyedPort>()
  let lines = new Map<object, KeyedPort>()
  // The last key message, given to a line armed later; seq lets the shared
  // worker apply a message once however many lines deliver it.
  let current: KeyMessage | null = null
  let seq = 0
  let statsListener: ((senders: Record<string, FrameStats>) => void) | null = null

  function sharedWorker(): Worker | null {
    if (!canArm()) return null
    worker ??= spawnWorker(senders => statsListener?.(senders))
    return worker
  }

  function arm(target: TransformTarget, direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource, layer?: number, dtx?: boolean): boolean {
    if (!isValidArmTarget(direction, channelId, senderId, source)) return false
    const currentWorker = sharedWorker()
    if (!currentWorker) return false
    const channel = new MessageChannel()
    try {
      target.transform = new RTCRtpScriptTransform(currentWorker, transformOptions(channel.port2, direction, channelId, senderId, source, layer, dtx), [channel.port2])
    } catch (error) {
      // The line stays unprotected, which every caller already refuses; the
      // rest of the negotiation (and the other lines) must not be lost to it.
      console.warn('[media] Could not arm a line:', error)
      closeQuietly(channel.port1)
      return false
    }
    const replaced = armed.get(target)
    if (replaced) releasePort(ports, lines, replaced)
    ports.add(channel.port1)
    armed.set(target, channel.port1)
    if (direction === 'recv' && target.transform) lines.set(target.transform, channel.port1)
    // A browser that cannot clone the key message (it holds CryptoKeys) leaves
    // the line without a key: it opens and seals nothing until setKey reaches it.
    if (current) postOrWarn(channel.port1, current)
    return true
  }

  function armIncoming(receiver: RTCRtpReceiver, channelId: string, senderId: string | null, source?: FrameSource): boolean {
    const port = receiver.transform ? lines.get(receiver.transform) : undefined
    // Browsers only apply a receiver transform that was set before the line
    // started receiving, so a prepared receiver keeps its transform and is
    // told who publishes on it and what it carries.
    if (port) {
      if (senderId === null) return true
      port.postMessage({ type: 'sender', senderId, ...(source === undefined ? {} : { source }) })
      return true
    }
    return arm(receiver, 'recv', channelId, senderId, source)
  }

  return {
    armSender: (sender, channelId, senderId, source, layer, dtx) => arm(sender, 'send', channelId, senderId, source, layer, dtx),
    armReceiver: armIncoming,
    setKey(epoch, key, replace = false, soft = false) {
      seq += 1
      const message = keyMessage({ epoch, key, seq, replace, soft })
      current = message
      for (const port of ports) postOrWarn(port, message)
    },
    staleKeys() {
      for (const port of ports) port.postMessage({ type: 'stale' })
    },
    onStats(listener) { statsListener = listener },
    reset() {
      current = null
      for (const port of [...ports]) releasePort(ports, lines, port, true)
      armed = new WeakMap()
      lines = new Map()
      worker?.terminate()
      worker = null
    },
  }
}
