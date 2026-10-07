import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { IDBFactory, IDBDatabase, IDBOpenDBRequest, IDBVersionChangeEvent } from 'fake-indexeddb'
import { openCryptoStateStore } from './stateStore.ts'
import type { CryptoGroupRecord, CryptoSendInput, CryptoStateStore, FreshCryptoContext, OpenCryptoStateOptions } from './stateStore.ts'

// A lock-manager platform fixture; native same-origin locking is additionally
// verified by the Chromium harness documented in doc/crypto-state.md.
function lockManager(): Pick<LockManager, 'request'> {
  const owners = new Set<string>()
  function request<T>(name: string, callback: LockGrantedCallback<T>): Promise<Awaited<T>>
  function request<T>(name: string, options: LockOptions, callback: LockGrantedCallback<T>): Promise<Awaited<T>>
  async function request<T>(name: string, optionsOrCallback: LockOptions | LockGrantedCallback<T>, suppliedCallback?: LockGrantedCallback<T>): Promise<Awaited<T>> {
    const options = typeof optionsOrCallback === 'function' ? {} : optionsOrCallback
    const callback = typeof optionsOrCallback === 'function' ? optionsOrCallback : suppliedCallback
    if (!callback) throw new Error('Lock callback required')
    expect(options).toEqual({ mode: 'exclusive', ifAvailable: true })
    if (owners.has(name)) return await callback(null)
    owners.add(name)
    try { return await callback({ name, mode: 'exclusive' }) }
    finally { owners.delete(name) }
  }
  return { request }
}

function requireState(state: CryptoGroupRecord | undefined): CryptoGroupRecord {
  if (!state) throw new Error('Expected a persisted cryptographic state')
  return state
}

const namespace = { communityId: 'test-community', accountId: 'test-account', deviceId: 'test-device' }
const u8 = (...values: number[]) => new Uint8Array(values)
let indexedDB: IDBFactory
let locks: Pick<LockManager, 'request'>
let stores: CryptoStateStore[]
let serial: number
async function open(options: Partial<OpenCryptoStateOptions> = {}) {
  const store = await openCryptoStateStore({
    namespace, indexedDB, locks,
    establishFreshContext: async () => ({ contextId: `test-context-${++serial}`, state: u8(1) }),
    ...options
  })
  stores.push(store)
  return store
}
async function establish(store: CryptoStateStore, expectedRevision = 0) {
  return store.establishContext({ groupId: 'group', expectedRevision })
}
async function rawDatabase() {
  const name = `mnema-crypto-v1:${JSON.stringify([namespace.communityId, namespace.accountId, namespace.deviceId])}`
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(name)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}
async function rewriteSnapshot(operation: (tx: IDBTransaction) => void) {
  const db = await rawDatabase()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(['groups', 'outbox', 'usage'], 'readwrite')
      tx.oncomplete = () => resolve()
      tx.onabort = () => reject(tx.error)
      operation(tx)
    })
  } finally { db.close() }
}
function send(state: CryptoGroupRecord, changes: Partial<CryptoSendInput> = {}): CryptoSendInput {
  return { groupId: state.groupId, contextId: state.contextId, expectedRevision: state.revision, eventId: 'event', nextState: u8(2), ciphertext: u8(7, 8), ...changes }
}
beforeEach(() => { indexedDB = new IDBFactory(); locks = lockManager(); stores = []; serial = 0 })
afterEach(async () => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); await Promise.all(stores.map(store => store.close())) })

describe('cryptographic state persistence boundaries', () => {
  it('commits state and ciphertext together, then acknowledges only the saved event', async () => {
    const store = await open()
    const state = await establish(store)
    const event = await store.commitSend(send(state))
    expect(event).toMatchObject({ eventId: 'event', revision: 2 })
    expect(requireState(await store.loadState('group'))).toMatchObject({ revision: 2, state: u8(2) })
    expect(await store.listPending('group')).toEqual([event])
    expect(await store.acknowledge({ groupId: 'group', eventId: 'event', contextId: state.contextId })).toBe(true)
    expect(await store.acknowledge({ groupId: 'group', eventId: 'event', contextId: state.contextId })).toBe(false)
    expect(await store.listPending('group')).toEqual([])
    expect((requireState(await store.loadState('group'))).revision).toBe(2)
  })

  it('rolls back a state write if the subsequent outbox insertion aborts', async () => {
    const store = await open()
    const state = await establish(store)
    const original = IDBDatabase.prototype.transaction
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(function (this: IDBDatabase, ...args: Parameters<typeof original>) {
      const tx = original.apply(this, args)
      if (args[1] === 'readwrite') {
        const objectStore = tx.objectStore.bind(tx)
        tx.objectStore = name => {
          const result = objectStore(name)
          if (name === 'outbox') result.add = () => { tx.abort(); throw new Error('injected abort after group.put') }
          return result
        }
      }
      return tx
    })
    await expect(store.commitSend(send(state))).rejects.toThrow('injected abort')
    vi.restoreAllMocks()
    expect(requireState(await store.loadState('group'))).toEqual(state)
    expect(await store.listPending('group')).toEqual([])
    await expect(store.commitSend(send(state))).rejects.toMatchObject({ code: 'fresh-context-required' })
    const fresh = await establish(store, state.revision)
    expect(fresh.contextId).not.toBe(state.contextId)
    expect((await store.commitSend(send(fresh))).revision).toBe(3)
  })

  it('blocks sending after reopen but preserves exact committed ciphertext for retransmission', async () => {
    let store = await open()
    const state = await establish(store)
    const event = await store.commitSend(send(state))
    await store.close()
    store = await open()
    const reopened = requireState(await store.loadState('group'))
    await expect(store.commitSend(send(reopened, { eventId: 'new' }))).rejects.toMatchObject({ code: 'fresh-context-required' })
    expect(await store.listPending('group')).toEqual([event])
    const fresh = await establish(store, reopened.revision)
    expect(fresh.contextId).not.toBe(reopened.contextId)
    await expect(store.commitSend(send(fresh, { eventId: 'new' }))).resolves.toMatchObject({ revision: 4 })
  })

  it('allows one owner per device and independently namespaces other devices', async () => {
    const first = await open()
    await expect(open()).rejects.toMatchObject({ code: 'owned-by-another-tab' })
    const second = await open({ namespace: { ...namespace, deviceId: 'other-device' } })
    await establish(first)
    expect(await second.loadState('group')).toBeUndefined()
    await first.close()
    const replacement = await open()
    expect((requireState(await replacement.loadState('group'))).revision).toBe(1)
  })

  it('copies caller buffers before queueing and prevents event overwrite', async () => {
    const store = await open()
    const state = await establish(store)
    const input = send(state)
    const pending = store.commitSend(input)
    input.nextState[0] = 99; input.ciphertext[0] = 99
    const event = await pending
    expect((requireState(await store.loadState('group'))).state).toEqual(u8(2))
    expect(event.ciphertext).toEqual(u8(7, 8))
    event.ciphertext[0] = 42
    const current = requireState(await store.loadState('group'))
    await expect(store.commitSend(send(current))).rejects.toMatchObject({ code: 'event-already-committed' })
    expect((await store.listPending('group'))[0]?.ciphertext).toEqual(u8(7, 8))
    expect((requireState(await store.loadState('group'))).revision).toBe(2)
  })

  it('rejects competing sends from the same revision without advancing state twice', async () => {
    const store = await open()
    const state = await establish(store)
    const results = await Promise.allSettled([
      store.commitSend(send(state, { eventId: 'one' })),
      store.commitSend(send(state, { eventId: 'two' }))
    ])
    expect(results[0]?.status).toBe('fulfilled')
    const second = results[1]
    if (!second || second.status !== 'rejected') throw new Error('Competing send must be rejected')
    expect(second.reason).toMatchObject({ code: 'state-conflict' })
    expect((requireState(await store.loadState('group'))).revision).toBe(2)
    expect((await store.listPending('group')).map(event => event.eventId)).toEqual(['one'])
    await expect(store.commitSend(send(requireState(await store.loadState('group')), { eventId: 'three' }))).rejects.toMatchObject({ code: 'fresh-context-required' })
  })

  it('enforces aggregate outbox quotas without dropping the previously committed event', async () => {
    const store = await open({ limits: { outboxEvents: 1, outboxBytes: 2 } })
    const state = await establish(store)
    const event = await store.commitSend(send(state))
    await expect(store.commitSend(send(requireState(await store.loadState('group')), { eventId: 'two' }))).rejects.toMatchObject({ code: 'storage-budget-exceeded' })
    expect(await store.listPending('group')).toEqual([event])
    expect((requireState(await store.loadState('group'))).revision).toBe(2)
    await store.acknowledge({ groupId: 'group', eventId: 'event', contextId: event.contextId })
    const fresh = await establish(store, 2)
    await expect(store.commitSend(send(fresh, { eventId: 'two' }))).resolves.toBeDefined()
  })

  it('bounds state size, group count, pending reads and queued operations', async () => {
    const store = await open({ limits: { stateBytes: 2, totalStateBytes: 2, groups: 1, queuedOperations: 1 } })
    const state = await establish(store)
    await expect(store.commitSend(send(state, { nextState: u8(1, 2, 3) }))).rejects.toMatchObject({ code: 'invalid-bytes' })
    await establish(store, 1)
    await expect(store.establishContext({ groupId: 'other', expectedRevision: 0 })).rejects.toMatchObject({ code: 'storage-budget-exceeded' })
    await expect(store.listPending('group', 257)).rejects.toMatchObject({ code: 'invalid-limit' })
    const pending = store.loadState('group')
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'operation-queue-full' })
    await pending
  })

  it('refuses old-context acknowledgements and context-ID reuse', async () => {
    const store = await open({ establishFreshContext: async () => ({ contextId: 'fixed-test-context', state: u8(1) }) })
    const state = await establish(store)
    await store.commitSend(send(state))
    await expect(store.acknowledge({ groupId: 'group', eventId: 'event', contextId: 'other-context' })).rejects.toMatchObject({ code: 'acknowledgement-context-conflict' })
    expect(await store.listPending('group')).toHaveLength(1)
    await expect(establish(store, 2)).rejects.toMatchObject({ code: 'context-reuse' })
  })

  it('persists receive-only state changes through the same revision boundary', async () => {
    const store = await open()
    const state = await establish(store)
    const next = await store.commitState({ groupId: 'group', contextId: state.contextId, expectedRevision: state.revision, nextState: u8(3) })
    expect(next).toMatchObject({ revision: 2, state: u8(3) })
    expect(await store.listPending('group')).toEqual([])
    await expect(store.commitState({ groupId: 'group', contextId: state.contextId, expectedRevision: 1, nextState: u8(4) })).rejects.toMatchObject({ code: 'state-conflict' })
  })

  it('fails closed when strict durability is unavailable', async () => {
    const store = await open()
    const original = IDBDatabase.prototype.transaction
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(function (this: IDBDatabase, ...args: Parameters<typeof original>) {
      const tx = original.apply(this, args)
      Object.defineProperty(tx, 'durability', { value: 'default' })
      return tx
    })
    await expect(establish(store)).rejects.toMatchObject({ code: 'strict-durability-unavailable' })
    vi.restoreAllMocks()
    expect(await store.loadState('group')).toBeUndefined()
  })

  it('requires native ownership and a trusted fresh-context provider', async () => {
    // Intentionally malformed platform inputs exercise runtime validation across the JS boundary.
    vi.stubGlobal('navigator', {})
    await expect(openCryptoStateStore({ namespace, indexedDB, establishFreshContext: () => ({ contextId: 'fresh', state: u8(1) }) })).rejects.toMatchObject({ code: 'ownership-unavailable' })
    await expect(Reflect.apply(openCryptoStateStore, undefined, [{ namespace, indexedDB, locks, establishFreshContext: null }])).rejects.toMatchObject({ code: 'fresh-context-provider-required' })
    await expect(open({ limits: { outboxEvents: Infinity } })).rejects.toMatchObject({ code: 'invalid-limit' })
    await expect(open({ namespace: { ...namespace, deviceId: '' } })).rejects.toMatchObject({ code: 'invalid-id' })
    const store = await open()
    await store.close()
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'store-closed' })
  })

  it('cancels a stalled provider on close, then releases ownership', async () => {
    let providerStarted: () => void = () => { throw new Error('Provider-start fixture uninitialized') }
    const started = new Promise<void>(resolve => { providerStarted = resolve })
    let signal: AbortSignal | undefined
    const store = await open({ establishFreshContext: args => {
      signal = args.signal
      providerStarted()
      return new Promise<FreshCryptoContext>(() => {})
    } })
    const pending = establish(store)
    await started
    const rejected = expect(pending).rejects.toMatchObject({ code: 'context-establishment-aborted' })
    await store.close()
    await rejected
    expect(signal?.aborted).toBe(true)
    const next = await open()
    expect(await next.loadState('group')).toBeUndefined()
  })

  it('bounds provider execution and discards a late result', async () => {
    let resolveProvider: (context: FreshCryptoContext) => void = () => { throw new Error('Provider resolver uninitialized') }
    const store = await open({ contextTimeoutMs: 5, establishFreshContext: () => new Promise(resolve => { resolveProvider = resolve }) })
    await expect(establish(store)).rejects.toMatchObject({ code: 'context-establishment-aborted' })
    resolveProvider({ contextId: 'too-late', state: u8(1) })
    expect(await store.loadState('group')).toBeUndefined()
  })

  it('does not treat a valid restored browser snapshot as a safe live sender', async () => {
    let store = await open()
    const snapshot = await establish(store)
    await store.commitSend(send(snapshot))
    await store.close()
    // Simulates external restoration of an internally consistent older browser
    // snapshot. No local counter can distinguish this from the original state.
    await rewriteSnapshot(tx => {
      tx.objectStore('groups').put(snapshot)
      tx.objectStore('outbox').clear()
      tx.objectStore('usage').put({ stateBytes: 1, eventBytes: 0, eventCount: 0, groupCount: 1 }, 'totals')
    })
    store = await open()
    expect(requireState(await store.loadState('group'))).toEqual(snapshot)
    await expect(store.commitSend(send(snapshot))).rejects.toMatchObject({ code: 'fresh-context-required' })
    const fresh = await establish(store, snapshot.revision)
    expect(fresh.contextId).not.toBe(snapshot.contextId)
    await expect(store.commitSend(send(fresh))).resolves.toBeDefined()
  })

  it('invalidates live readiness when persisted state is malformed', async () => {
    const store = await open()
    const state = await establish(store)
    await rewriteSnapshot(tx => { tx.objectStore('groups').put({ ...state, schemaVersion: 99 }) })
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'invalid-stored-state' })
    await rewriteSnapshot(tx => { tx.objectStore('groups').put(state) })
    await expect(store.commitSend(send(state))).rejects.toMatchObject({ code: 'fresh-context-required' })
  })

  it('closes on a schema upgrade and refuses opening an unsupported future schema', async () => {
    const store = await open()
    await establish(store)
    const name = `mnema-crypto-v1:${JSON.stringify([namespace.communityId, namespace.accountId, namespace.deviceId])}`
    await new Promise<void>((resolve, reject) => {
      const request = indexedDB.open(name, 2)
      request.onsuccess = () => { request.result.close(); resolve() }
      request.onerror = () => reject(request.error)
    })
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'store-closed' })
    await store.close()
    await expect(open()).rejects.toMatchObject({ code: 'storage-open-failed' })
    // A failed open must release the Web Lock, rather than strand the device.
    await expect(open()).rejects.toMatchObject({ code: 'storage-open-failed' })
  })

  it('invalidates provider readiness if transaction construction itself throws', async () => {
    const store = await open()
    const state = await establish(store)
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementationOnce(() => { throw new DOMException('synthetic closed database', 'InvalidStateError') })
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'storage-transaction-unavailable' })
    vi.restoreAllMocks()
    await expect(store.commitSend(send(state))).rejects.toMatchObject({ code: 'fresh-context-required' })
    expect((requireState(await store.loadState('group'))).revision).toBe(1)
  })

  it.each([0, -1, 30001, NaN])('rejects invalid provider timeout %s before acquiring storage', async contextTimeoutMs => {
    await expect(open({ contextTimeoutMs })).rejects.toMatchObject({ code: 'invalid-timeout' })
  })

  it('fails closed if the browser has no IndexedDB and releases device ownership', async () => {
    vi.stubGlobal('indexedDB', undefined)
    await expect(openCryptoStateStore({ namespace, locks, establishFreshContext: () => ({ contextId: 'fresh', state: u8(1) }) })).rejects.toMatchObject({ code: 'storage-unavailable' })
    await expect(open()).resolves.toBeDefined()
  })

  it('reports browser lock failures without attempting to open a database', async () => {
    vi.spyOn(locks, 'request').mockRejectedValueOnce(new Error('synthetic browser lock failure'))
    const opening = vi.spyOn(indexedDB, 'open')
    await expect(open()).rejects.toMatchObject({ code: 'ownership-failed' })
    expect(opening).not.toHaveBeenCalled()
  })

  it('rejects unsafe revisions, identifiers, empty state and invalid pending limits', async () => {
    const store = await open()
    const state = await establish(store)
    await expect(store.loadState('')).rejects.toMatchObject({ code: 'invalid-id' })
    await expect(store.establishContext({ groupId: 'group', expectedRevision: Number.MAX_SAFE_INTEGER })).rejects.toMatchObject({ code: 'invalid-revision' })
    await expect(store.establishContext({ groupId: 'group', expectedRevision: 0 })).rejects.toMatchObject({ code: 'state-conflict' })
    await expect(store.commitState({ groupId: 'group', contextId: state.contextId, expectedRevision: -1, nextState: u8(2) })).rejects.toMatchObject({ code: 'invalid-revision' })
    await expect(store.commitState({ groupId: 'group', contextId: state.contextId, expectedRevision: 1, nextState: u8() })).rejects.toMatchObject({ code: 'invalid-bytes' })
    await expect(store.acknowledge({ groupId: '', eventId: 'event', contextId: state.contextId })).rejects.toMatchObject({ code: 'invalid-id' })
    for (const limit of [0, -1, NaN, 1.5]) await expect(store.listPending('group', limit)).rejects.toMatchObject({ code: 'invalid-limit' })
    expect(await store.loadState('missing')).toBeUndefined()
  })

  it('rejects queued reads after close before touching the database', async () => {
    const store = await open()
    const pending = store.loadState('group')
    const rejection = expect(pending).rejects.toMatchObject({ code: 'store-closed' })
    await store.close()
    await rejection
  })

  it('reports a failed browser request and invalidates the live sender', async () => {
    const store = await open()
    const state = await establish(store)
    const original = IDBDatabase.prototype.transaction
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementationOnce(function (this: IDBDatabase, ...args: Parameters<typeof original>) {
      const tx = original.apply(this, args)
      const objectStore = tx.objectStore.bind(tx)
      tx.objectStore = name => {
        const result = objectStore(name)
        const get = result.get.bind(result)
        result.get = query => {
          const request = get(query)
          queueMicrotask(() => tx.abort())
          return request
        }
        return result
      }
      return tx
    })
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'storage-request-failed' })
    vi.restoreAllMocks()
    await expect(store.commitSend(send(state))).rejects.toMatchObject({ code: 'fresh-context-required' })
    expect(requireState(await store.loadState('group'))).toEqual(state)
  })

  it('rejects corrupt stored usage and over-budget totals before advancing a sender', async () => {
    const store = await open({ limits: { outboxBytes: 2 } })
    const state = await establish(store)
    await rewriteSnapshot(tx => { tx.objectStore('usage').put({ stateBytes: -1, eventBytes: 0, eventCount: 0, groupCount: 1 }, 'totals') })
    await expect(store.commitSend(send(state))).rejects.toMatchObject({ code: 'invalid-stored-usage' })
    await rewriteSnapshot(tx => { tx.objectStore('usage').put({ stateBytes: 1, eventBytes: 3, eventCount: 0, groupCount: 1 }, 'totals') })
    await expect(establish(store, 1)).rejects.toMatchObject({ code: 'storage-budget-exceeded' })
    expect(requireState(await store.loadState('group'))).toEqual(state)
  })

  it('rejects missing stored usage and malformed outbox schema rather than retransmitting it', async () => {
    const store = await open()
    const state = await establish(store)
    const event = await store.commitSend(send(state))
    await rewriteSnapshot(tx => { tx.objectStore('outbox').put({ ...event, schemaVersion: 99 }) })
    await expect(store.listPending('group')).rejects.toMatchObject({ code: 'invalid-stored-event' })
    await rewriteSnapshot(tx => {
      tx.objectStore('outbox').put(event)
      tx.objectStore('usage').delete('totals')
    })
    await expect(store.acknowledge({ groupId: 'group', eventId: event.eventId, contextId: event.contextId })).rejects.toMatchObject({ code: 'invalid-stored-usage' })
    expect(await store.listPending('group')).toEqual([event])
  })

  it('detects an external state change while the fresh-context provider is running', async () => {
    let previous: CryptoGroupRecord | undefined
    const store = await open({ establishFreshContext: async request => {
      if (request.previous) {
        previous = request.previous
        await rewriteSnapshot(tx => { tx.objectStore('groups').put({ ...request.previous, revision: 2 }) })
      }
      return { contextId: `fresh-${++serial}`, state: u8(1) }
    } })
    await establish(store)
    await expect(establish(store, 1)).rejects.toMatchObject({ code: 'state-conflict' })
    expect(previous?.revision).toBe(1)
    expect(requireState(await store.loadState('group')).revision).toBe(2)
    expect(await store.listPending('group')).toEqual([])
  })

  it('rejects malformed provider outputs without creating sender state', async () => {
    const store = await open({ establishFreshContext: () => ({ contextId: '', state: u8(1) }) })
    await expect(establish(store)).rejects.toMatchObject({ code: 'invalid-id' })
    expect(await store.loadState('group')).toBeUndefined()
  })

  it('closes when the browser unexpectedly closes the database', async () => {
    const connections: IDBDatabase[] = []
    const original = IDBDatabase.prototype.transaction
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(function (this: IDBDatabase, ...args: Parameters<typeof original>) {
      connections.push(this)
      return original.apply(this, args)
    })
    const store = await open()
    await establish(store)
    const database = connections.at(-1)
    if (!database) throw new Error('Expected live browser database')
    database.onclose?.(new Event('close'))
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'store-closed' })
    await store.close()
    expect(await open()).toBeDefined()
  })

  it('bounds database opening, ignores late success and abandons late upgrades', async () => {
    vi.useFakeTimers()
    const request = new IDBOpenDBRequest()
    vi.spyOn(indexedDB, 'open').mockReturnValueOnce(request)
    const pending = open()
    const rejection = expect(pending).rejects.toMatchObject({ code: 'storage-open-timeout' })
    await vi.advanceTimersByTimeAsync(5000)
    await rejection
    vi.useRealTimers()
    const db = await rawDatabase()
    const close = vi.spyOn(db, 'close')
    const abort = vi.fn()
    Object.defineProperty(request, 'result', { value: db })
    Object.defineProperty(request, 'transaction', { value: { abort } })
    request.onupgradeneeded?.(new IDBVersionChangeEvent('upgradeneeded', { oldVersion: 0, newVersion: 1 }))
    request.onsuccess?.(new Event('success'))
    expect(abort).toHaveBeenCalledOnce()
    expect(close).toHaveBeenCalledOnce()
    vi.useRealTimers()
  })

  it('reports blocked database upgrades immediately and releases ownership', async () => {
    const request = new IDBOpenDBRequest()
    vi.spyOn(indexedDB, 'open').mockReturnValueOnce(request)
    const pending = open()
    const rejection = expect(pending).rejects.toMatchObject({ code: 'storage-upgrade-blocked' })
    await vi.waitFor(() => expect(request.onblocked).toBeTypeOf('function'))
    request.onblocked?.(new IDBVersionChangeEvent('blocked', { oldVersion: 1, newVersion: 2 }))
    await rejection
    expect(await open()).toBeDefined()
  })
})
