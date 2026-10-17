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

function readStats(data: unknown): Record<string, FrameStats> | null {
  if (!data || typeof data !== 'object' || !('type' in data) || data.type !== 'stats' || !('senders' in data)) return null
  const raw = data.senders
  if (!raw || typeof raw !== 'object') return null
  const out: Record<string, FrameStats> = {}
  for (const [id, entry] of Object.entries(raw)) {
    if (!entry || typeof entry !== 'object' || !('opened' in entry) || !('newer' in entry) || !('epoch' in entry)) continue
    const { opened, newer, epoch } = entry
    if (count(opened) && count(newer) && typeof epoch === 'number' && Number.isInteger(epoch)) out[id] = { opened, newer, epoch }
  }
  return out
}

function canArm(): boolean {
  return typeof Worker === 'function' && typeof RTCRtpScriptTransform === 'function' && typeof MessageChannel === 'function'
}

/** One worker seals every local track and opens every remote track of this call. */
export function createMediaGuard(): MediaGuard {
  let worker: Worker | null = null
  const ports: KeyedPort[] = []
  let armed = new WeakMap<RTCRtpReceiver, KeyedPort>()
  // The last key message, given to a line armed later; seq lets the shared
  // worker apply a message once however many lines deliver it.
  let current: { type: 'key', epoch: number, key: EpochKeys, replace?: true, soft?: true, caps?: FrameCaps, seq: number } | null = null
  // The newest advertisement, given to a line armed later after the key.
  let currentCaps: { type: 'caps', epoch: number, caps: FrameCaps, seq: number } | null = null
  let seq = 0
  let statsListener: ((senders: Record<string, FrameStats>) => void) | null = null

  function sharedWorker(): Worker | null {
    if (!canArm()) return null
    if (!worker) {
      worker = new Worker(new URL('./mediaFrameWorker.ts', import.meta.url), { type: 'module' })
      worker.onmessage = (event: MessageEvent<unknown>) => {
        const senders = readStats(event.data)
        if (senders) statsListener?.(senders)
      }
    }
    return worker
  }

  function arm(target: TransformTarget, direction: 'send' | 'recv', channelId: string, senderId: string | null, source?: FrameSource, layer?: number): boolean {
    if (!channelId || senderId === '' || (senderId === null && direction === 'send') || (direction === 'send' && source === undefined)) return false
    const currentWorker = sharedWorker()
    if (!currentWorker) return false
    const channel = new MessageChannel()
    target.transform = new RTCRtpScriptTransform(currentWorker, {
      port: channel.port2,
      direction,
      channelId,
      ...(senderId === null ? {} : { senderId }),
      ...(source === undefined ? {} : { source }),
      ...(layer === undefined ? {} : { layer }),
    }, [channel.port2])
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
      const message = {
        type: 'key' as const, epoch, key, seq,
        ...(replace ? { replace: true as const } : {}),
        ...(soft && !replace ? { soft: true as const } : {}),
        ...(caps ? { caps } : {}),
      }
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
        try { port.postMessage({ type: 'drop' }) } catch { /* already closed */ }
        try { port.close() } catch { /* already closed */ }
      }
      ports.length = 0
      armed = new WeakMap()
      worker?.terminate()
      worker = null
    },
  }
}
