import { decodeTrustPreview, decodeTrustStatus, trustOperationId, type NativeTrustPresentationPort, type TrustPreview, type TrustStatus } from './native/nativeTrust.ts'
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
const invalidReply = 'Invalid native trust reply'

function failReply(): never {
  throw new Error(invalidReply)
}

function isTrustObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== 'object') return false
  if (value === null) return false
  return !Array.isArray(value)
}

function trustReplyRecord(value: unknown): Record<string, unknown> {
  if (!isTrustObject(value)) failReply()
  if (Object.keys(value).length !== 3) failReply()
  if (!('context' in value)) failReply()
  return value
}

function httpStatusCode(value: unknown): number {
  if (typeof value !== 'number') failReply()
  if (!Number.isInteger(value)) failReply()
  if (value < 100) failReply()
  if (value > 599) failReply()
  return value
}

function reply(value: unknown, context: string): Reply {
  const record = trustReplyRecord(value)
  if (record['context'] !== context) failReply()
  if (!('status' in record)) failReply()
  const status = httpStatusCode(record['status'])
  if (!('body' in record)) failReply()
  return { context, status, body: record['body'] }
}

function cancelled(): DOMException {
  return new DOMException('Native trust presentation retired', 'AbortError')
}

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

function current(bridge: CapturedTrustBridge, st: PortState): void {
  if (st.broken) throw cancelled()
  if (!bridge.isCurrent()) throw cancelled()
}

function publish(bridge: CapturedTrustBridge, st: PortState, status: TrustStatus): void {
  if (st.broken) return
  if (!bridge.isCurrent()) return
  if (status.operation_id !== st.operation) return
  if (st.terminal) return
  if (status.state !== 'pending') st.terminal = status.state
  for (const listener of [...st.listeners]) listener({ ...status })
}

async function cancelKnown(bridge: CapturedTrustBridge, st: PortState, id: string): Promise<void> {
  const value = reply(await bridge.invoke('native_trust_cancel', { context: bridge.context, operationId: id }), bridge.context)
  if (value.status === 204 && value.body === null) return
  if (value.status !== 200) throw new Error('Native trust cancellation unavailable')
  const status = decodeTrustStatus(value.body)
  if (status.operation_id !== id || !['cancelled', 'root_saved', 'expired', 'failed'].includes(status.state)) throw new Error('Invalid native trust cancellation')
  publish(bridge, st, status)
}

function receive(bridge: CapturedTrustBridge, st: PortState, attempt: number, value: unknown): void {
  if (attempt !== st.incarnation) return
  if (st.broken) return
  if (!bridge.isCurrent()) return
  try {
    acceptStatus(bridge, st, value)
  } catch {
    breakPresentation(bridge, st)
  }
}

function bufferingEarly(st: PortState): boolean {
  if (!st.preparing) return false
  return st.operation === null
}

function acceptStatus(bridge: CapturedTrustBridge, st: PortState, value: unknown): void {
  const status = decodeTrustStatus(value)
  if (status.state === 'device_saved') throw new Error('Wrong native operation status')
  if (!bufferingEarly(st)) {
    publish(bridge, st, status)
    return
  }
  bufferEarlyStatus(st, status)
}

function bufferEarlyStatus(st: PortState, status: TrustStatus): void {
  if (st.buffered.length >= 32) throw new Error('Native status buffer exceeded')
  st.buffered.push(status)
}

function breakPresentation(bridge: CapturedTrustBridge, st: PortState): void {
  st.broken = true
  st.buffered = []
  if (!st.operation) return
  void cancelKnown(bridge, st, st.operation).catch(() => {})
}

function setupAlreadyPrepared(st: PortState): boolean {
  if (st.preparing) return true
  if (st.operation === null) return false
  if (st.terminal === null) return true
  return st.terminal === 'root_saved'
}

function watchStatus(bridge: CapturedTrustBridge, st: PortState, attempt: number): StatusChannel {
  // Allocate only when invoking native Begin. A ended Rust Channel cannot
  // receive a later setup, so every attempt gets a fresh callback identity.
  const channel = bridge.channel()
  channel.onmessage = value => { receive(bridge, st, attempt, value) }
  return channel
}

function replay(bridge: CapturedTrustBridge, st: PortState, pending: readonly TrustStatus[]): void {
  for (const status of pending) publish(bridge, st, status)
}

function scheduleReplay(bridge: CapturedTrustBridge, st: PortState, pending: readonly TrustStatus[]): void {
  // Begin resolves before replaying status so the component owns its preview.
  queueMicrotask(() => { queueMicrotask(() => { replay(bridge, st, pending) }) })
}

type RootProblem = 'wrong' | 'retired'

function rootProblem(bridge: CapturedTrustBridge, st: PortState, preview: TrustPreview, channelId: string): RootProblem | null {
  if (preview.operation_kind !== 'first_root') return 'wrong'
  if (preview.scope.channel_id !== channelId) return 'wrong'
  if (st.broken) return 'retired'
  if (!bridge.isCurrent()) return 'retired'
  return null
}

async function rejectRoot(bridge: CapturedTrustBridge, st: PortState, operationId: string, problem: RootProblem): Promise<never> {
  await cancelKnown(bridge, st, operationId)
  if (problem === 'wrong') throw new Error('Wrong native root operation')
  throw cancelled()
}

async function beginFirstRoot(bridge: CapturedTrustBridge, st: PortState, channelId: string): Promise<TrustPreview> {
  current(bridge, st)
  if (!trustOperationId(channelId)) throw new Error('Native setup already prepared')
  if (setupAlreadyPrepared(st)) throw new Error('Native setup already prepared')
  st.operation = null
  st.terminal = null
  st.buffered = []
  st.preparing = true
  const attempt = ++st.incarnation
  try {
    const channel = watchStatus(bridge, st, attempt)
    const value = reply(await bridge.invoke('native_trust_begin_first_root', { context: bridge.context, channelId, onStatus: channel }), bridge.context)
    if (value.status !== 200) throw new Error('Native root setup unavailable')
    const preview = decodeTrustPreview(value.body)
    const problem = rootProblem(bridge, st, preview, channelId)
    if (problem !== null) await rejectRoot(bridge, st, preview.operation_id, problem)
    st.operation = preview.operation_id
    const pending = st.buffered
    st.buffered = []
    scheduleReplay(bridge, st, pending)
    return preview
  } finally { st.preparing = false }
}

async function requestConfirmation(bridge: CapturedTrustBridge, st: PortState, operationId: string): Promise<TrustStatus> {
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

async function readCurrentStatus(bridge: CapturedTrustBridge, st: PortState): Promise<TrustStatus | null> {
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
