import type { EpochKeys, FrameSource } from './frame'

export interface FrameCaps { members: string[], complete: boolean }

export interface FrameStats { opened: number, newer: number, epoch: number }

interface KeyedPort {
  postMessage(data: unknown): void
  close(): void
}

export interface MediaGuard {
  armSender(sender: RTCRtpSender, channelId: string, senderId: string, source: FrameSource, layer?: number): boolean
  /**
   * Arms a receiver. With no sender yet it opens nothing until bindReceiver
   * names the publisher. Arming an armed receiver only names the publisher
   * and what its line carries now.
   */
  armReceiver(receiver: RTCRtpReceiver, channelId: string, senderId: string | null, source?: FrameSource): boolean
  /**
   * With soft, the epoch was reached by a commit that removed nobody: sealing may stay one moment on the epoch left.
   * `caps` says who opens frame version 3 in the epoch and whether that is everyone.
   */
  setKey(epoch: number, key: EpochKeys, replace?: boolean, soft?: boolean, caps?: FrameCaps): void
  /** Members of the epoch that advertised frame version 3 changed; the keys did not. */
  setCaps(epoch: number, caps: FrameCaps): void
  /** The call lost its group: the last keys may seal only a little longer. */
  staleKeys(): void
  /** Per publisher: frames opened, and frames from an epoch newer than any key held. */
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

function isStatsEntry(entry: unknown): entry is { opened: unknown, newer: unknown, epoch: unknown } {
  if (!entry || typeof entry !== 'object') return false
  if (!('opened' in entry) || !('newer' in entry)) return false
  return 'epoch' in entry
}

function statsOf(entry: { opened: unknown, newer: unknown, epoch: unknown }): FrameStats | null {
  const { opened, newer, epoch } = entry
  if (!count(opened) || !count(newer)) return null
  if (typeof epoch !== 'number' || !Number.isInteger(epoch)) return null
  return { opened, newer, epoch }
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

function canArm(): boolean {
  return typeof Worker === 'function' && typeof RTCRtpScriptTransform === 'function' && typeof MessageChannel === 'function'
}

type KeyMessage = { type: 'key', epoch: number, key: EpochKeys, replace?: true, soft?: true, caps?: FrameCaps, seq: number }

interface KeyFields {
  epoch: number
  key: EpochKeys
  seq: number
  replace: boolean
  soft: boolean
  caps: FrameCaps | undefined
}

function keyMessage(fields: KeyFields): KeyMessage {
  const { epoch, key, seq, replace, soft, caps } = fields
  return {
    type: 'key' as const, epoch, key, seq,
    ...(replace ? { replace: true as const } : {}),
    ...(soft && !replace ? { soft: true as const } : {}),
    ...(caps ? { caps } : {}),
  }
}

/** What a line's transform worker is told about it when it is armed. */
function transformOptions(port: MessagePort, direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource, layer?: number) {
  return {
    port,
    direction,
    channelId,
    ...(senderId === null ? {} : { senderId }),
    ...(source === undefined ? {} : { source }),
    ...(layer === undefined ? {} : { layer }),
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

function isValidArmTarget(direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource): boolean {
  if (!channelId || senderId === '') return false
  if (direction === 'send') {
    return senderId !== null && source !== undefined
  }
  return true
}

/** One worker seals every local track and opens every remote track of this call. */
export function createMediaGuard(): MediaGuard {
  let worker: Worker | null = null
  const ports: KeyedPort[] = []
  let armed = new WeakMap<RTCRtpReceiver, KeyedPort>()
  // The last key message, given to a line armed later; seq lets the shared
  // worker apply a message once however many lines deliver it.
  let current: KeyMessage | null = null
  // The newest advertisement, given to a line armed later after the key.
  let currentCaps: { type: 'caps', epoch: number, caps: FrameCaps, seq: number } | null = null
  let seq = 0
  let statsListener: ((senders: Record<string, FrameStats>) => void) | null = null

  function sharedWorker(): Worker | null {
    if (!canArm()) return null
    worker ??= spawnWorker(senders => statsListener?.(senders))
    return worker
  }

  function arm(target: TransformTarget, direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource, layer?: number): boolean {
    if (!isValidArmTarget(direction, channelId, senderId, source)) return false
    const currentWorker = sharedWorker()
    if (!currentWorker) return false
    const channel = new MessageChannel()
    target.transform = new RTCRtpScriptTransform(currentWorker, transformOptions(channel.port2, direction, channelId, senderId, source, layer), [channel.port2])
    if (current) channel.port1.postMessage(current)
    if (currentCaps && current && currentCaps.epoch === current.epoch) channel.port1.postMessage(currentCaps)
    ports.push(channel.port1)
    if (direction === 'recv') armed.set(target as RTCRtpReceiver, channel.port1)
    return true
  }

  function armIncoming(receiver: RTCRtpReceiver, channelId: string, senderId: string | null, source?: FrameSource): boolean {
    const port = armed.get(receiver)
    // Browsers only apply a receiver transform that was set before the line
    // started receiving, so a prepared receiver keeps its transform and is
    // told who publishes on it and what it carries.
    if (port && receiver.transform) {
      if (senderId === null) return true
      port.postMessage({ type: 'sender', senderId, ...(source === undefined ? {} : { source }) })
      return true
    }
    return arm(receiver, 'recv', channelId, senderId, source)
  }

  return {
    armSender: (sender, channelId, senderId, source, layer) => arm(sender, 'send', channelId, senderId, source, layer),
    armReceiver: armIncoming,
    setKey(epoch, key, replace = false, soft = false, caps) {
      seq += 1
      const message = keyMessage({ epoch, key, seq, replace, soft, caps })
      current = message
      currentCaps = null
      for (const port of ports) port.postMessage(message)
    },
    setCaps(epoch, caps) {
      seq += 1
      const message = { type: 'caps' as const, epoch, caps, seq }
      currentCaps = message
      for (const port of ports) port.postMessage(message)
    },
    staleKeys() {
      for (const port of ports) port.postMessage({ type: 'stale' })
    },
    onStats(listener) { statsListener = listener },
    reset() {
      current = null
      currentCaps = null
      for (const port of ports) {
        try { port.postMessage({ type: 'drop' }) } catch (error) { ignoreClosedPort(error) }
        try { port.close() } catch (error) { ignoreClosedPort(error) }
      }
      ports.length = 0
      armed = new WeakMap()
      worker?.terminate()
      worker = null
    },
  }
}
