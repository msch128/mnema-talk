interface KeyedPort {
  postMessage(data: unknown): void
  close(): void
}

export interface MediaGuard {
  armSender(sender: RTCRtpSender, channelId: string, senderId: string): boolean
  armReceiver(receiver: RTCRtpReceiver, channelId: string, senderId: string): boolean
  setKey(epoch: number, key: CryptoKey, replace?: boolean): void
  clearKeys(): void
  reset(): void
}

interface TransformTarget {
  transform: RTCRtpScriptTransform | null
}

function canArm(): boolean {
  return typeof Worker === 'function' && typeof RTCRtpScriptTransform === 'function' && typeof MessageChannel === 'function'
}

/** One worker seals every local track and opens every remote track of this call. */
export function createMediaGuard(): MediaGuard {
  let worker: Worker | null = null
  const ports: KeyedPort[] = []
  let current: { epoch: number, key: CryptoKey } | null = null

  function sharedWorker(): Worker | null {
    if (!canArm()) return null
    worker ??= new Worker(new URL('./mediaFrameWorker.ts', import.meta.url), { type: 'module' })
    return worker
  }

  function arm(target: TransformTarget, direction: 'send' | 'recv', channelId: string, senderId: string): boolean {
    if (!channelId || !senderId) return false
    const currentWorker = sharedWorker()
    if (!currentWorker) return false
    const channel = new MessageChannel()
    target.transform = new RTCRtpScriptTransform(currentWorker, {
      port: channel.port2,
      direction,
      channelId,
      senderId,
    }, [channel.port2])
    if (current) channel.port1.postMessage({ type: 'key', epoch: current.epoch, key: current.key })
    ports.push(channel.port1)
    return true
  }

  return {
    armSender: (sender, channelId, senderId) => arm(sender, 'send', channelId, senderId),
    armReceiver: (receiver, channelId, senderId) => arm(receiver, 'recv', channelId, senderId),
    setKey(epoch, key, replace = false) {
      current = { epoch, key }
      const message = replace
        ? { type: 'key', epoch, key, replace: true }
        : { type: 'key', epoch, key }
      for (const port of ports) port.postMessage(message)
    },
    clearKeys() {
      current = null
      for (const port of ports) port.postMessage({ type: 'drop' })
    },
    reset() {
      current = null
      for (const port of ports) {
        try { port.postMessage({ type: 'drop' }) } catch { /* already closed */ }
        try { port.close() } catch { /* already closed */ }
      }
      ports.length = 0
      worker?.terminate()
      worker = null
    },
  }
}
