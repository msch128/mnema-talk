import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { IDBFactory, IDBDatabase } from 'fake-indexeddb'
import { openCryptoStateStore } from './stateStore.js'

// A lock-manager platform fixture; native same-origin locking is additionally
// verified by the Chromium harness documented in doc/crypto-state.md.
function lockManager() {
  const owners = new Set()
  return {
    async request(name, options, callback) {
      expect(options).toEqual({ mode: 'exclusive', ifAvailable: true })
      if (owners.has(name)) return callback(null)
      owners.add(name)
      try { return await callback({ name, mode: 'exclusive' }) }
      finally { owners.delete(name) }
    }
  }
}

const namespace = { communityId: 'test-community', accountId: 'test-account', deviceId: 'test-device' }
const u8 = (...values) => new Uint8Array(values)
let indexedDB, locks, stores, serial
async function open(options = {}) {
  const store = await openCryptoStateStore({
    namespace, indexedDB, locks,
    establishFreshContext: async () => ({ contextId: `test-context-${++serial}`, state: u8(1) }),
    ...options
  })
  stores.push(store)
  return store
}
async function establish(store, expectedRevision = 0) {
  return store.establishContext({ groupId: 'group', expectedRevision })
}
async function rawDatabase() {
  const name = `mnema-crypto-v1:${JSON.stringify([namespace.communityId, namespace.accountId, namespace.deviceId])}`
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(name)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}
async function rewriteSnapshot(operation) {
  const db = await rawDatabase()
  try {
    await new Promise((resolve, reject) => {
      const tx = db.transaction(['groups', 'outbox', 'usage'], 'readwrite')
      tx.oncomplete = resolve
      tx.onabort = () => reject(tx.error)
      operation(tx)
    })
  } finally { db.close() }
}
function send(state, changes = {}) {
  return { groupId: state.groupId, contextId: state.contextId, expectedRevision: state.revision, eventId: 'event', nextState: u8(2), ciphertext: u8(7, 8), ...changes }
}
beforeEach(() => { indexedDB = new IDBFactory(); locks = lockManager(); stores = []; serial = 0 })
afterEach(async () => { vi.restoreAllMocks(); await Promise.all(stores.map(store => store.close())) })

describe('cryptographic state persistence boundaries', () => {
  it('commits state and ciphertext together, then acknowledges only the saved event', async () => {
    const store = await open()
    const state = await establish(store)
    const event = await store.commitSend(send(state))
    expect(event).toMatchObject({ eventId: 'event', revision: 2 })
    expect(await store.loadState('group')).toMatchObject({ revision: 2, state: u8(2) })
    expect(await store.listPending('group')).toEqual([event])
    expect(await store.acknowledge({ groupId: 'group', eventId: 'event', contextId: state.contextId })).toBe(true)
    expect(await store.acknowledge({ groupId: 'group', eventId: 'event', contextId: state.contextId })).toBe(false)
    expect(await store.listPending('group')).toEqual([])
    expect((await store.loadState('group')).revision).toBe(2)
  })

  it('rolls back a state write if the subsequent outbox insertion aborts', async () => {
    const store = await open()
    const state = await establish(store)
    const original = IDBDatabase.prototype.transaction
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(function (...args) {
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
    expect(await store.loadState('group')).toEqual(state)
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
    const reopened = await store.loadState('group')
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
    expect((await replacement.loadState('group')).revision).toBe(1)
  })

  it('copies caller buffers before queueing and prevents event overwrite', async () => {
    const store = await open()
    const state = await establish(store)
    const input = send(state)
    const pending = store.commitSend(input)
    input.nextState[0] = 99; input.ciphertext[0] = 99
    const event = await pending
    expect((await store.loadState('group')).state).toEqual(u8(2))
    expect(event.ciphertext).toEqual(u8(7, 8))
    event.ciphertext[0] = 42
    const current = await store.loadState('group')
    await expect(store.commitSend(send(current))).rejects.toMatchObject({ code: 'event-already-committed' })
    expect((await store.listPending('group'))[0].ciphertext).toEqual(u8(7, 8))
    expect((await store.loadState('group')).revision).toBe(2)
  })

  it('rejects competing sends from the same revision without advancing state twice', async () => {
    const store = await open()
    const state = await establish(store)
    const results = await Promise.allSettled([
      store.commitSend(send(state, { eventId: 'one' })),
      store.commitSend(send(state, { eventId: 'two' }))
    ])
    expect(results[0].status).toBe('fulfilled')
    expect(results[1].reason).toMatchObject({ code: 'state-conflict' })
    expect((await store.loadState('group')).revision).toBe(2)
    expect((await store.listPending('group')).map(event => event.eventId)).toEqual(['one'])
    await expect(store.commitSend(send(await store.loadState('group'), { eventId: 'three' }))).rejects.toMatchObject({ code: 'fresh-context-required' })
  })

  it('enforces aggregate outbox quotas without dropping the previously committed event', async () => {
    const store = await open({ limits: { outboxEvents: 1, outboxBytes: 2 } })
    const state = await establish(store)
    const event = await store.commitSend(send(state))
    await expect(store.commitSend(send(await store.loadState('group'), { eventId: 'two' }))).rejects.toMatchObject({ code: 'storage-budget-exceeded' })
    expect(await store.listPending('group')).toEqual([event])
    expect((await store.loadState('group')).revision).toBe(2)
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
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(function (...args) {
      const tx = original.apply(this, args)
      Object.defineProperty(tx, 'durability', { value: 'default' })
      return tx
    })
    await expect(establish(store)).rejects.toMatchObject({ code: 'strict-durability-unavailable' })
    vi.restoreAllMocks()
    expect(await store.loadState('group')).toBeUndefined()
  })

  it('requires native ownership and a trusted fresh-context provider', async () => {
    await expect(open({ locks: {} })).rejects.toMatchObject({ code: 'ownership-unavailable' })
    await expect(open({ establishFreshContext: null })).rejects.toMatchObject({ code: 'fresh-context-provider-required' })
    await expect(open({ limits: { outboxEvents: Infinity } })).rejects.toMatchObject({ code: 'invalid-limit' })
    await expect(open({ namespace: { ...namespace, deviceId: '' } })).rejects.toMatchObject({ code: 'invalid-id' })
    const store = await open()
    await store.close()
    await expect(store.loadState('group')).rejects.toMatchObject({ code: 'store-closed' })
  })

  it('cancels a stalled provider on close, then releases ownership', async () => {
    let providerStarted
    const started = new Promise(resolve => { providerStarted = resolve })
    let signal
    const store = await open({ establishFreshContext: args => {
      signal = args.signal
      providerStarted()
      return new Promise(() => {})
    } })
    const pending = establish(store)
    await started
    const rejected = expect(pending).rejects.toMatchObject({ code: 'context-establishment-aborted' })
    await store.close()
    await rejected
    expect(signal.aborted).toBe(true)
    const next = await open()
    expect(await next.loadState('group')).toBeUndefined()
  })

  it('bounds provider execution and discards a late result', async () => {
    let resolveProvider
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
    expect(await store.loadState('group')).toEqual(snapshot)
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
    await new Promise((resolve, reject) => {
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
    expect((await store.loadState('group')).revision).toBe(1)
  })
})
