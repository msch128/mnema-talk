import { decodeTrustPreview, decodeTrustStatus, trustOperationId, type NativeTrustPresentationPort, type TrustStatus } from './nativeTrust.ts'
export interface StatusChannel { onmessage: (value: unknown) => void }
/** Captured document/profile callbacks. These checks protect presentation only;
 * native scope checks and an actual OS callback alone grant cryptographic trust.
 */
export interface CapturedTrustBridge {
  readonly context: string
  isCurrent(): boolean
  invoke(command: string, args: Record<string, unknown>): Promise<unknown>
  channel(): StatusChannel
}
type Reply = { context: string; status: number; body: unknown }
function reply(value: unknown, context: string): Reply {
  if (typeof value !== 'object' || value === null || Array.isArray(value) || Object.keys(value).length !== 3 || !('context' in value) || value.context !== context || !('status' in value) || typeof value.status !== 'number' || !Number.isInteger(value.status) || value.status < 100 || value.status > 599 || !('body' in value)) throw new Error('Invalid native trust reply')
  return { context, status: value.status, body: value.body }
}
const cancelled = () => new DOMException('Native trust presentation retired', 'AbortError')
export function createTrustPort(bridge: CapturedTrustBridge): NativeTrustPresentationPort & { readCurrentStatus(): Promise<TrustStatus | null> } {
  if (!trustOperationId(bridge.context)) throw new Error('Invalid native presentation context')
  const listeners = new Set<(value: unknown) => void>()
  let operation: string | null = null
  let preparing = false
  let broken = false
  let terminal: TrustStatus['state'] | null = null
  let buffered: TrustStatus[] = []
  let incarnation = 0
  function current() { if (broken || !bridge.isCurrent()) throw cancelled() }
  function publish(status: TrustStatus) {
    if (broken || !bridge.isCurrent() || status.operation_id !== operation || terminal) return
    if (status.state !== 'pending') terminal = status.state
    for (const listener of [...listeners]) listener({ ...status })
  }
  async function cancelKnown(id: string) {
    const value = reply(await bridge.invoke('native_trust_cancel', { context: bridge.context, operationId: id }), bridge.context)
    if (value.status === 204 && value.body === null) return
    if (value.status !== 200) throw new Error('Native trust cancellation unavailable')
    const status = decodeTrustStatus(value.body)
    if (status.operation_id !== id || !['cancelled', 'root_saved', 'expired', 'failed'].includes(status.state)) throw new Error('Invalid native trust cancellation')
    publish(status)
  }
  function receive(attempt: number, value: unknown) {
    if (attempt !== incarnation || broken || !bridge.isCurrent()) return
    try {
      const status = decodeTrustStatus(value)
      if (status.state === 'device_saved') throw new Error('Wrong native operation status')
      if (preparing && operation === null) {
        if (buffered.length >= 32) throw new Error('Native status buffer exceeded')
        buffered.push(status)
      } else publish(status)
    } catch {
      broken = true; buffered = []
      if (operation) void cancelKnown(operation).catch(() => {})
    }
  }
  return {
    subscribe(listener) { current(); listeners.add(listener); return () => { listeners.delete(listener) } },
    async beginFirstRoot(channelId) {
      current()
      if (!trustOperationId(channelId) || preparing || (operation !== null && (terminal === null || terminal === 'root_saved'))) throw new Error('Native setup already prepared')
      operation = null; terminal = null; buffered = []
      preparing = true
      const attempt = ++incarnation
      try {
        // Allocate only when invoking native Begin. A ended Rust Channel cannot
        // receive a later setup, so every attempt gets a fresh callback identity.
        const channel = bridge.channel()
        channel.onmessage = value => receive(attempt, value)
        const value = reply(await bridge.invoke('native_trust_begin_first_root', { context: bridge.context, channelId, onStatus: channel }), bridge.context)
        if (value.status !== 200) throw new Error('Native root setup unavailable')
        const preview = decodeTrustPreview(value.body)
        if (preview.operation_kind !== 'first_root' || preview.scope.channel_id !== channelId) {
          await cancelKnown(preview.operation_id)
          throw new Error('Wrong native root operation')
        }
        if (broken || !bridge.isCurrent()) {
          await cancelKnown(preview.operation_id)
          throw cancelled()
        }
        operation = preview.operation_id
        const pending = buffered; buffered = []
        // Begin resolves before replaying status so the component owns its preview.
        queueMicrotask(() => { queueMicrotask(() => { for (const status of pending) publish(status) }) })
        return preview
      } finally { preparing = false }
    },
    async requestConfirmation(operationId) {
      current()
      if (operationId !== operation || terminal) throw new Error('Native operation unavailable')
      const value = reply(await bridge.invoke('native_trust_request_confirmation', { context: bridge.context, operationId }), bridge.context)
      current()
      if (value.status !== 200) throw new Error('Native confirmation unavailable')
      const status = decodeTrustStatus(value.body)
      if (status.operation_id !== operationId || !['pending', 'cancelled'].includes(status.state)) throw new Error('Invalid confirmation acknowledgement')
      publish(status)
      return status
    },
    async cancel(operationId) {
      if (!trustOperationId(operationId) || operationId !== operation) throw new Error('Unknown native operation')
      // A saved operation's cancellation is a native no-op, not Core retirement.
      await cancelKnown(operationId)
    },
    async readCurrentStatus() {
      current()
      const value = reply(await bridge.invoke('native_trust_read_status', { context: bridge.context }), bridge.context)
      current()
      if (value.status === 204 && value.body === null) return null
      if (value.status !== 200) throw new Error('Native status unavailable')
      const status = decodeTrustStatus(value.body)
      if (status.operation_id !== operation) throw new Error('Wrong native status operation')
      publish(status)
      return status
    }
  }
}
