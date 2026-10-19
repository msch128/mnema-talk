import type { StoredUsage, CryptoGroupRecord, CryptoPendingEvent, FreshCryptoContext, CryptoStateLimits, CryptoNamespace, FreshContextRequest, OpenCryptoStateOptions, CryptoStateUpdate, CryptoSendInput, CryptoStateStore } from './stateStoreTypes'
export type { CryptoNamespace, CryptoGroupRecord, CryptoPendingEvent, FreshContextRequest, FreshCryptoContext, CryptoStateLimits, OpenCryptoStateOptions, CryptoStateUpdate, CryptoSendInput, CryptoStateStore } from './stateStoreTypes'

// Persistence only. This module neither encrypts nor authenticates devices.
// The provider must not publish anything until commitSend has resolved.
// (One self-contained module on purpose: the browser harness in e2e/ serves it alone.)

const SCHEMA_VERSION = 1
const DEFAULT_LIMITS: Readonly<CryptoStateLimits> = Object.freeze({
  stateBytes: 8 * 1024 * 1024,
  totalStateBytes: 16 * 1024 * 1024,
  ciphertextBytes: 1024 * 1024,
  outboxBytes: 8 * 1024 * 1024,
  outboxEvents: 256,
  groups: 256,
  queuedOperations: 32
})

export class CryptoStateError extends Error {
  readonly code: string
  constructor(code: string) {
    super(`Cryptographic state operation failed: ${code}`)
    this.name = 'CryptoStateError'
    this.code = code
  }
}

function fail(code: string): never { throw new CryptoStateError(code) }
function id(value: unknown): string {
  if (typeof value !== 'string' || !value.length || value.length > 256) fail('invalid-id')
  return value
}
function revision(value: unknown): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0 || value === Number.MAX_SAFE_INTEGER) fail('invalid-revision')
  return value
}
function bytes(value: unknown, maximum: number): Uint8Array<ArrayBuffer> {
  if (!(value instanceof Uint8Array) || !value.byteLength || value.byteLength > maximum) fail('invalid-bytes')
  return new Uint8Array(value)
}
function requestResult<T>(request: IDBRequest<T>): Promise<unknown> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(new CryptoStateError('storage-request-failed'))
  })
}

async function acquireOwner(locks: Pick<LockManager, 'request'> | undefined, name: string): Promise<() => Promise<void>> {
  if (typeof locks?.request !== 'function') fail('ownership-unavailable')
  let release: () => void = () => {}
  const held = new Promise<void>(resolve => { release = resolve })
  let finished: Promise<void> = Promise.resolve()
  await new Promise<void>((resolve, reject) => {
    // ifAvailable is intentional: another tab is an explicit conflict, not an
    // unbounded queue. Never steal a lock from a live cryptographic provider.
    finished = Promise.resolve().then(() => locks.request(name, { mode: 'exclusive', ifAvailable: true }, lock => {
      if (!lock) {
        reject(new CryptoStateError('owned-by-another-tab'))
        return
      }
      resolve()
      return held
    })).catch(() => { reject(new CryptoStateError('ownership-failed')) })
  })
  return async () => { release(); await finished }
}

function openDatabase(indexedDB: IDBFactory | undefined, name: string): Promise<IDBDatabase> {
  if (typeof indexedDB?.open !== 'function') fail('storage-unavailable')
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(name, SCHEMA_VERSION)
    let abandoned = false
    const timeout = setTimeout(() => {
      abandoned = true
      reject(new CryptoStateError('storage-open-timeout'))
    }, 5000)
    request.onblocked = () => {
      clearTimeout(timeout)
      abandoned = true
      reject(new CryptoStateError('storage-upgrade-blocked'))
    }
    request.onerror = () => {
      clearTimeout(timeout)
      reject(new CryptoStateError('storage-open-failed'))
    }
    request.onupgradeneeded = () => {
      if (abandoned) { request.transaction?.abort(); return }
      const db = request.result
      db.createObjectStore('groups', { keyPath: 'groupId' })
      db.createObjectStore('outbox', { keyPath: ['groupId', 'eventId'] }).createIndex('group', 'groupId')
      db.createObjectStore('usage').put({ stateBytes: 0, eventBytes: 0, eventCount: 0, groupCount: 0 }, 'totals')
    }
    request.onsuccess = () => {
      clearTimeout(timeout)
      if (abandoned) { request.result.close(); return }
      resolve(request.result)
    }
  })
}

/** Everything one open store shares: the database, the live contexts, the queue and the close state. */
interface StoreCtx {
  readonly scope: Readonly<CryptoNamespace>
  readonly limits: CryptoStateLimits
  readonly db: IDBDatabase
  /** NEVER persisted: reopening always requires fresh authorization. */
  readonly active: Map<string, string>
  readonly transactions: Set<IDBTransaction>
  readonly providerCalls: Set<AbortController>
  readonly establishFreshContext: (request: FreshContextRequest) => FreshCryptoContext | Promise<FreshCryptoContext>
  readonly contextTimeoutMs: number
  readonly releaseOwner: () => Promise<void>
  closed: boolean
  closePromise: Promise<void> | undefined
  tail: Promise<void>
  queued: number
}

function enqueue<T>(ctx: StoreCtx, operation: () => T | Promise<T>): Promise<T> {
  if (ctx.closed) return Promise.reject(new CryptoStateError('store-closed'))
  if (ctx.queued >= ctx.limits.queuedOperations) return Promise.reject(new CryptoStateError('operation-queue-full'))
  ctx.queued++
  const result = ctx.tail.then(() => {
    if (ctx.closed) fail('store-closed')
    return operation()
  }).finally(() => { ctx.queued-- })
  ctx.tail = result.then(() => {}, () => {})
  return result
}

async function transaction<T>(ctx: StoreCtx, stores: string[], write: boolean, operation: (tx: IDBTransaction) => T | Promise<T>): Promise<T> {
  let tx: IDBTransaction
  try {
    tx = ctx.db.transaction(stores, write ? 'readwrite' : 'readonly', write ? { durability: 'strict' } : undefined)
  } catch {
    ctx.active.clear()
    fail('storage-transaction-unavailable')
  }
  ctx.transactions.add(tx)
  const completion = new Promise<void>((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onabort = () => reject(new CryptoStateError('storage-transaction-aborted'))
    tx.onerror = () => {} // Request failures abort; never preventDefault.
  })
  // Attach before requests to avoid an unhandled rejection on an early abort.
  completion.catch(() => {})
  try {
    if (write && tx.durability !== 'strict') fail('strict-durability-unavailable')
    const result = await operation(tx)
    await completion
    return result
  } catch (error) {
    try { tx.abort() } catch { /* Already aborted or committed. */ }
    await completion.catch(() => {})
    ctx.active.clear() // Corruption/uncertain persistence invalidates all live provider contexts.
    throw error
  } finally { ctx.transactions.delete(tx) }
}

function storedObject(value: unknown, code: string): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) fail(code)
  return value as Record<string, unknown>
}

function validateGroup(ctx: StoreCtx, value: unknown): CryptoGroupRecord | undefined {
  if (value === undefined) return undefined
  const record = storedObject(value, 'invalid-stored-state')
  if (record.schemaVersion !== SCHEMA_VERSION) fail('invalid-stored-state')
  return { schemaVersion: SCHEMA_VERSION, groupId: id(record.groupId), contextId: id(record.contextId), revision: revision(record.revision), state: bytes(record.state, ctx.limits.stateBytes) }
}

function validateEvent(ctx: StoreCtx, value: unknown): CryptoPendingEvent {
  const record = storedObject(value, 'invalid-stored-event')
  if (record.schemaVersion !== SCHEMA_VERSION) fail('invalid-stored-event')
  return { schemaVersion: SCHEMA_VERSION, groupId: id(record.groupId), contextId: id(record.contextId), eventId: id(record.eventId), revision: revision(record.revision), ciphertext: bytes(record.ciphertext, ctx.limits.ciphertextBytes) }
}

function validateUsage(ctx: StoreCtx, value: unknown): StoredUsage {
  const { limits } = ctx
  const record = storedObject(value, 'invalid-stored-usage')
  const checked: StoredUsage = { stateBytes: 0, eventBytes: 0, eventCount: 0, groupCount: 0 }
  for (const key of ['stateBytes', 'eventBytes', 'eventCount', 'groupCount'] as const) {
    const count = record[key]
    if (typeof count !== 'number' || !Number.isSafeInteger(count) || count < 0) fail('invalid-stored-usage')
    checked[key] = count
  }
  if (checked.stateBytes > limits.totalStateBytes || checked.eventBytes > limits.outboxBytes || checked.eventCount > limits.outboxEvents || checked.groupCount > limits.groups) fail('storage-budget-exceeded')
  return checked
}

async function readGroup(ctx: StoreCtx, tx: IDBTransaction, groupId: string) {
  return validateGroup(ctx, await requestResult(tx.objectStore('groups').get(groupId)))
}

async function readUsage(ctx: StoreCtx, tx: IDBTransaction) {
  return validateUsage(ctx, await requestResult(tx.objectStore('usage').get('totals')))
}

function assertCurrent(ctx: StoreCtx, record: CryptoGroupRecord | undefined, expectedRevision: number, contextId: string) {
  if (!record || record.revision !== expectedRevision || record.contextId !== contextId) fail('state-conflict')
  if (ctx.active.get(record.groupId) !== contextId) fail('fresh-context-required')
}

function writeState(ctx: StoreCtx, tx: IDBTransaction, usage: StoredUsage, record: CryptoGroupRecord | undefined, state: Uint8Array<ArrayBuffer>, contextId: string, groupId: string): CryptoGroupRecord {
  usage.stateBytes += state.byteLength - (record?.state.byteLength || 0)
  if (!record) usage.groupCount++
  validateUsage(ctx, usage)
  const next: CryptoGroupRecord = { schemaVersion: SCHEMA_VERSION, groupId, contextId, revision: (record?.revision || 0) + 1, state }
  revision(next.revision)
  tx.objectStore('groups').put(next)
  return next
}

function closeStore(ctx: StoreCtx) {
  if (ctx.closePromise) return ctx.closePromise
  ctx.closed = true
  ctx.active.clear()
  for (const controller of ctx.providerCalls) controller.abort()
  for (const tx of ctx.transactions) { try { tx.abort() } catch { /* Already complete. */ } }
  ctx.db.close()
  // Retain the lock until pending writes and queued work have settled.
  ctx.closePromise = ctx.tail.then(ctx.releaseOwner)
  return ctx.closePromise
}

async function freshContext(ctx: StoreCtx, groupId: string, previous: CryptoGroupRecord | undefined): Promise<FreshCryptoContext> {
  const controller = new AbortController()
  ctx.providerCalls.add(controller)
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      Promise.resolve().then(() => ctx.establishFreshContext({ namespace: ctx.scope, groupId, previous, signal: controller.signal })),
      new Promise<never>((_, reject) => {
        controller.signal.addEventListener('abort', () => reject(new CryptoStateError('context-establishment-aborted')), { once: true })
        timer = setTimeout(() => controller.abort(), ctx.contextTimeoutMs)
      })
    ])
  } catch (error) {
    controller.abort()
    throw error
  } finally {
    clearTimeout(timer)
    ctx.providerCalls.delete(controller)
  }
}

// Persistence only. This module neither encrypts nor authenticates devices.
// The provider must not publish anything until commitSend has resolved.

function loadState(ctx: StoreCtx, groupId: string) {
  try { id(groupId) } catch (error) { return Promise.reject(error) }
  return enqueue(ctx, () => transaction(ctx, ['groups'], false, tx => readGroup(ctx, tx, groupId)))
}

function establishContext(ctx: StoreCtx, { groupId, expectedRevision }: { groupId: string; expectedRevision: number }) {
  return enqueue(ctx, async () => {
    ctx.active.delete(groupId)
    id(groupId); revision(expectedRevision)
    const previous = await transaction(ctx, ['groups'], false, tx => readGroup(ctx, tx, groupId))
    if ((previous?.revision || 0) !== expectedRevision) fail('state-conflict')
    // Crypto/network work MUST happen outside the IDB transaction.
    const fresh = await freshContext(ctx, groupId, previous)
    const contextId = id(fresh?.contextId)
    const state = bytes(fresh?.state, ctx.limits.stateBytes)
    if (previous?.contextId === contextId) fail('context-reuse')
    if (ctx.closed) fail('store-closed')
    const saved = await transaction(ctx, ['groups', 'usage'], true, async tx => {
      const current = await readGroup(ctx, tx, groupId)
      if ((current?.revision || 0) !== expectedRevision) fail('state-conflict')
      const usage = await readUsage(ctx, tx)
      const next = writeState(ctx, tx, usage, current, state, contextId, groupId)
      tx.objectStore('usage').put(usage, 'totals')
      return next
    })
    if (ctx.closed) fail('store-closed')
    ctx.active.set(groupId, contextId)
    return saved
  })
}

function commitSend(ctx: StoreCtx, input: CryptoSendInput) {
  // Copy synchronously so callers cannot mutate bytes while work is queued.
  let send: { groupId: string; contextId: string; expectedRevision: number; eventId: string; state: Uint8Array<ArrayBuffer>; ciphertext: Uint8Array<ArrayBuffer> }
  try {
    send = { groupId: id(input?.groupId), contextId: id(input?.contextId), expectedRevision: revision(input?.expectedRevision), eventId: id(input?.eventId), state: bytes(input?.nextState, ctx.limits.stateBytes), ciphertext: bytes(input?.ciphertext, ctx.limits.ciphertextBytes) }
  } catch (error) { ctx.active.clear(); return Promise.reject(error) }
  return enqueue(ctx, () => transaction(ctx, ['groups', 'outbox', 'usage'], true, async tx => {
    const current = await readGroup(ctx, tx, send.groupId)
    assertCurrent(ctx, current, send.expectedRevision, send.contextId)
    if (await requestResult(tx.objectStore('outbox').get([send.groupId, send.eventId]))) fail('event-already-committed')
    const usage = await readUsage(ctx, tx)
    usage.eventCount++
    usage.eventBytes += send.ciphertext.byteLength
    const next = writeState(ctx, tx, usage, current, send.state, send.contextId, send.groupId)
    const event: CryptoPendingEvent = { schemaVersion: SCHEMA_VERSION, groupId: send.groupId, contextId: send.contextId, eventId: send.eventId, revision: next.revision, ciphertext: send.ciphertext }
    tx.objectStore('outbox').add(event)
    tx.objectStore('usage').put(usage, 'totals')
    return event
  })).catch(error => { ctx.active.clear(); throw error })
}

function commitState(ctx: StoreCtx, { groupId, contextId, expectedRevision, nextState }: CryptoStateUpdate) {
  let state: Uint8Array<ArrayBuffer>
  try { id(groupId); id(contextId); revision(expectedRevision); state = bytes(nextState, ctx.limits.stateBytes) }
  catch (error) { ctx.active.clear(); return Promise.reject(error) }
  return enqueue(ctx, () => transaction(ctx, ['groups', 'usage'], true, async tx => {
    const current = await readGroup(ctx, tx, groupId)
    assertCurrent(ctx, current, expectedRevision, contextId)
    const usage = await readUsage(ctx, tx)
    const next = writeState(ctx, tx, usage, current, state, contextId, groupId)
    tx.objectStore('usage').put(usage, 'totals')
    return next
  })).catch(error => { ctx.active.clear(); throw error })
}

function listPending(ctx: StoreCtx, groupId: string, limit = ctx.limits.outboxEvents) {
  try { id(groupId); if (!Number.isSafeInteger(limit) || limit < 1 || limit > ctx.limits.outboxEvents) fail('invalid-limit') }
  catch (error) { return Promise.reject(error) }
  return enqueue(ctx, () => transaction(ctx, ['outbox'], false, async tx => {
    const events = await requestResult(tx.objectStore('outbox').index('group').getAll(groupId, limit))
    if (!Array.isArray(events)) fail('invalid-stored-event')
    return events.map(event => validateEvent(ctx, event))
  }))
}

function acknowledge(ctx: StoreCtx, { groupId, eventId, contextId }: { groupId: string; eventId: string; contextId: string }) {
  try { id(groupId); id(eventId); id(contextId) } catch (error) { return Promise.reject(error) }
  return enqueue(ctx, () => transaction(ctx, ['outbox', 'usage'], true, async tx => {
    const stored = await requestResult(tx.objectStore('outbox').get([groupId, eventId]))
    if (!stored) return false
    const record = validateEvent(ctx, stored)
    if (record.contextId !== contextId) fail('acknowledgement-context-conflict')
    const usage = await readUsage(ctx, tx)
    usage.eventCount--; usage.eventBytes -= record.ciphertext.byteLength
    validateUsage(ctx, usage)
    tx.objectStore('outbox').delete([groupId, eventId])
    tx.objectStore('usage').put(usage, 'totals')
    return true
  }))
}

/** The namespace and limits an open store is configured with, validated. */
function validatedConfig({ namespace, establishFreshContext, limits: overrides = {}, contextTimeoutMs = 30000 }: OpenCryptoStateOptions) {
  const scope = Object.freeze({
    communityId: id(namespace?.communityId),
    accountId: id(namespace?.accountId),
    deviceId: id(namespace?.deviceId)
  })
  if (typeof establishFreshContext !== 'function') fail('fresh-context-provider-required')
  if (!Number.isSafeInteger(contextTimeoutMs) || contextTimeoutMs < 1 || contextTimeoutMs > 30000) fail('invalid-timeout')
  const limits = { ...DEFAULT_LIMITS, ...overrides }
  for (const [key, value] of Object.entries(limits)) {
    if (!Object.hasOwn(DEFAULT_LIMITS, key) || !Number.isSafeInteger(value) || value < 1 || value > DEFAULT_LIMITS[key as keyof CryptoStateLimits]) fail('invalid-limit')
  }
  return { scope, limits, contextTimeoutMs }
}

/**
 * Own one community/account/device store. establishFreshContext is a TRUSTED
 * provider boundary, not a backend response verifier supplied by the server.
 * It must create fresh, authorized sender state independently of a restored
 * local snapshot. Merely returning a new identifier is insufficient.
 */
export async function openCryptoStateStore(options: OpenCryptoStateOptions): Promise<CryptoStateStore> {
  const { indexedDB = globalThis.indexedDB, locks = globalThis.navigator?.locks } = options
  const { scope, limits, contextTimeoutMs } = validatedConfig(options)
  const name = `mnema-crypto-v1:${JSON.stringify([scope.communityId, scope.accountId, scope.deviceId])}`
  const releaseOwner = await acquireOwner(locks, name)
  let db: IDBDatabase
  try { db = await openDatabase(indexedDB, name) } catch (error) { await releaseOwner(); throw error }
  const ctx: StoreCtx = {
    scope, limits, db, releaseOwner, contextTimeoutMs,
    establishFreshContext: options.establishFreshContext,
    active: new Map(), // NEVER persist readiness; reopening always requires fresh authorization.
    transactions: new Set(),
    providerCalls: new Set(),
    closed: false,
    closePromise: undefined,
    tail: Promise.resolve(),
    queued: 0
  }
  const close = () => closeStore(ctx)
  db.onversionchange = () => { void close() }
  db.onclose = () => { void close() }

  return Object.freeze({
    close,
    namespace: scope,
    loadState: (groupId: string) => loadState(ctx, groupId),
    establishContext: (input: { groupId: string; expectedRevision: number }) => establishContext(ctx, input),
    commitSend: (input: CryptoSendInput) => commitSend(ctx, input),
    commitState: (update: CryptoStateUpdate) => commitState(ctx, update),
    listPending: (groupId: string, limit?: number) => listPending(ctx, groupId, limit),
    acknowledge: (input: { groupId: string; eventId: string; contextId: string }) => acknowledge(ctx, input)
  })
}
