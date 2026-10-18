export interface CryptoNamespace {
  readonly communityId: string
  readonly accountId: string
  readonly deviceId: string
}
export interface CryptoGroupRecord {
  readonly schemaVersion: 1
  readonly groupId: string
  readonly contextId: string
  readonly revision: number
  readonly state: Uint8Array<ArrayBuffer>
}
export interface CryptoPendingEvent {
  readonly schemaVersion: 1
  readonly groupId: string
  readonly contextId: string
  readonly eventId: string
  readonly revision: number
  readonly ciphertext: Uint8Array<ArrayBuffer>
}
export interface StoredUsage {
  stateBytes: number
  eventBytes: number
  eventCount: number
  groupCount: number
}
export interface FreshContextRequest {
  readonly namespace: Readonly<CryptoNamespace>
  readonly groupId: string
  readonly previous: CryptoGroupRecord | undefined
  readonly signal: AbortSignal
}
export interface FreshCryptoContext {
  readonly contextId: string
  readonly state: Uint8Array
}
export interface CryptoStateLimits {
  stateBytes: number
  totalStateBytes: number
  ciphertextBytes: number
  outboxBytes: number
  outboxEvents: number
  groups: number
  queuedOperations: number
}
export interface OpenCryptoStateOptions {
  namespace: CryptoNamespace
  establishFreshContext: (request: FreshContextRequest) => FreshCryptoContext | Promise<FreshCryptoContext>
  indexedDB?: IDBFactory
  locks?: Pick<LockManager, 'request'>
  limits?: Partial<CryptoStateLimits>
  contextTimeoutMs?: number
}
export interface CryptoStateUpdate {
  groupId: string
  contextId: string
  expectedRevision: number
  nextState: Uint8Array
}
export interface CryptoSendInput extends CryptoStateUpdate {
  eventId: string
  ciphertext: Uint8Array
}
export interface CryptoStateStore {
  readonly namespace: Readonly<CryptoNamespace>
  close(): Promise<void>
  loadState(groupId: string): Promise<CryptoGroupRecord | undefined>
  establishContext(input: { groupId: string; expectedRevision: number }): Promise<CryptoGroupRecord>
  commitSend(input: CryptoSendInput): Promise<CryptoPendingEvent>
  commitState(input: CryptoStateUpdate): Promise<CryptoGroupRecord>
  listPending(groupId: string, limit?: number): Promise<CryptoPendingEvent[]>
  acknowledge(input: { groupId: string; eventId: string; contextId: string }): Promise<boolean>
}
