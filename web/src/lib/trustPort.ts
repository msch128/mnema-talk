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

/** What one trust port remembers about the operation it presents. */
interface PortState {
  listeners: Set<(value: unknown) => void>
  operation: string | null
  preparing: boolean
  broken: boolean
  terminal: TrustStatus['state'] | null
  buffered: TrustStatus[]
  incarnation: number
}

function current(bridge: CapturedTrustBridge, st: PortState) {
  if (st.broken || !bridge.isCurrent()) throw cancelled()
}

function publish(bridge: CapturedTrustBridge, st: PortState, status: TrustStatus) {
  if (st.broken || !bridge.isCurrent() || status.operation_id !== st.operation || st.terminal) return
  if (status.state !== 'pending') st.terminal = status.state
  for (const listener of [...st.listeners]) listener({ ...status })
}

async function cancelKnown(bridge: CapturedTrustBridge, st: PortState, id: string) {
  const value = reply(await bridge.invoke('native_trust_cancel', { context: bridge.context, operationId: id }), bridge.context)
  if (value.status === 204 && value.body === null) return
  if (value.status !== 200) throw new Error('Native trust cancellation unavailable')
  const status = decodeTrustStatus(value.body)
  if (status.operation_id !== id || !['cancelled', 'root_saved', 'expired', 'failed'].includes(status.state)) throw new Error('Invalid native trust cancellation')
  publish(bridge, st, status)
}

function receive(bridge: CapturedTrustBridge, st: PortState, attempt: number, value: unknown) {
  if (attempt !== st.incarnation || st.broken || !bridge.isCurrent()) return
  try {
    const status = decodeTrustStatus(value)
    if (status.state === 'device_saved') throw new Error('Wrong native operation status')
    if (st.preparing && st.operation === null) {
      if (st.buffered.length >= 32) throw new Error('Native status buffer exceeded')
      st.buffered.push(status)
    } else publish(bridge, st, status)
  } catch {
    st.broken = true; st.buffered = []
    if (st.operation) void cancelKnown(bridge, st, st.operation).catch(() => {})
  }
}

async function beginFirstRoot(bridge: CapturedTrustBridge, st: PortState, channelId: string) {
  current(bridge, st)
  if (!trustOperationId(channelId) || st.preparing || (st.operation !== null && (st.terminal === null || st.terminal === 'root_saved'))) throw new Error('Native setup already prepared')
  st.operation = null; st.terminal = null; st.buffered = []
  st.preparing = true
  const attempt = ++st.incarnation
  try {
    // Allocate only when invoking native Begin. A ended Rust Channel cannot
    // receive a later setup, so every attempt gets a fresh callback identity.
    const channel = bridge.channel()
    channel.onmessage = value => receive(bridge, st, attempt, value)
    const value = reply(await bridge.invoke('native_trust_begin_first_root', { context: bridge.context, channelId, onStatus: channel }), bridge.context)
    if (value.status !== 200) throw new Error('Native root setup unavailable')
    const preview = decodeTrustPreview(value.body)
    if (preview.operation_kind !== 'first_root' || preview.scope.channel_id !== channelId) {
      await cancelKnown(bridge, st, preview.operation_id)
      throw new Error('Wrong native root operation')
    }
    if (st.broken || !bridge.isCurrent()) {
      await cancelKnown(bridge, st, preview.operation_id)
      throw cancelled()
    }
    st.operation = preview.operation_id
    const pending = st.buffered; st.buffered = []
    // Begin resolves before replaying status so the component owns its preview.
    queueMicrotask(() => { queueMicrotask(() => { for (const status of pending) publish(bridge, st, status) }) })
    return preview
  } finally { st.preparing = false }
}

async function requestConfirmation(bridge: CapturedTrustBridge, st: PortState, operationId: string) {
  current(bridge, st)
  if (operationId !== st.operation || st.terminal) throw new Error('Native operation unavailable')
  const value = reply(await bridge.invoke('native_trust_request_confirmation', { context: bridge.context, operationId }), bridge.context)
  current(bridge, st)
  if (value.status !== 200) throw new Error('Native confirmation unavailable')
  const status = decodeTrustStatus(value.body)
  if (status.operation_id !== operationId || !['pending', 'cancelled'].includes(status.state)) throw new Error('Invalid confirmation acknowledgement')
  publish(bridge, st, status)
  return status
}

async function readCurrentStatus(bridge: CapturedTrustBridge, st: PortState) {
  current(bridge, st)
  const value = reply(await bridge.invoke('native_trust_read_status', { context: bridge.context }), bridge.context)
  current(bridge, st)
  if (value.status === 204 && value.body === null) return null
  if (value.status !== 200) throw new Error('Native status unavailable')
  const status = decodeTrustStatus(value.body)
  if (status.operation_id !== st.operation) throw new Error('Wrong native status operation')
  publish(bridge, st, status)
  return status
}

export function createTrustPort(bridge: CapturedTrustBridge): NativeTrustPresentationPort & { readCurrentStatus(): Promise<TrustStatus | null> } {
  if (!trustOperationId(bridge.context)) throw new Error('Invalid native presentation context')
  const st: PortState = { listeners: new Set(), operation: null, preparing: false, broken: false, terminal: null, buffered: [], incarnation: 0 }
  return {
    subscribe(listener) { current(bridge, st); st.listeners.add(listener); return () => { st.listeners.delete(listener) } },
    beginFirstRoot: channelId => beginFirstRoot(bridge, st, channelId),
    requestConfirmation: operationId => requestConfirmation(bridge, st, operationId),
    async cancel(operationId) {
      if (!trustOperationId(operationId) || operationId !== st.operation) throw new Error('Unknown native operation')
      // A saved operation's cancellation is a native no-op, not Core retirement.
      await cancelKnown(bridge, st, operationId)
    },
    readCurrentStatus: () => readCurrentStatus(bridge, st)
  }
}
