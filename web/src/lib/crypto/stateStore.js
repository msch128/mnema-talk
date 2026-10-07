// Persistence only. This module neither encrypts nor authenticates devices.
// The provider must not publish anything until commitSend has resolved.
const SCHEMA_VERSION = 1
const DEFAULT_LIMITS = Object.freeze({
  stateBytes: 8 * 1024 * 1024,
  totalStateBytes: 16 * 1024 * 1024,
  ciphertextBytes: 1024 * 1024,
  outboxBytes: 8 * 1024 * 1024,
  outboxEvents: 256,
  groups: 256,
  queuedOperations: 32
})

export class CryptoStateError extends Error {
  constructor(code) {
    super(`Cryptographic state operation failed: ${code}`)
    this.name = 'CryptoStateError'
    this.code = code
  }
}

function fail(code) { throw new CryptoStateError(code) }
function id(value) {
  if (typeof value !== 'string' || !value.length || value.length > 256) fail('invalid-id')
  return value
}
function revision(value) {
  if (!Number.isSafeInteger(value) || value < 0 || value === Number.MAX_SAFE_INTEGER) fail('invalid-revision')
  return value
}
function bytes(value, maximum) {
  if (!(value instanceof Uint8Array) || !value.byteLength || value.byteLength > maximum) fail('invalid-bytes')
  return new Uint8Array(value)
}
function requestResult(request) {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(new CryptoStateError('storage-request-failed'))
  })
}

async function acquireOwner(locks, name) {
  if (typeof locks?.request !== 'function') fail('ownership-unavailable')
  let release
  const held = new Promise(resolve => { release = resolve })
  let finished
  await new Promise((resolve, reject) => {
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

function openDatabase(indexedDB, name) {
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
      if (abandoned) { request.transaction.abort(); return }
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

/**
 * Own one community/account/device store. establishFreshContext is a TRUSTED
 * provider boundary, not a backend response verifier supplied by the server.
 * It must create fresh, authorized sender state independently of a restored
 * local snapshot. Merely returning a new identifier is insufficient.
 */
export async function openCryptoStateStore({
  namespace,
  establishFreshContext,
  indexedDB = globalThis.indexedDB,
  locks = globalThis.navigator?.locks,
  limits: overrides = {},
  contextTimeoutMs = 30000
}) {
  const scope = Object.freeze({
    communityId: id(namespace?.communityId),
    accountId: id(namespace?.accountId),
    deviceId: id(namespace?.deviceId)
  })
  if (typeof establishFreshContext !== 'function') fail('fresh-context-provider-required')
  if (!Number.isSafeInteger(contextTimeoutMs) || contextTimeoutMs < 1 || contextTimeoutMs > 30000) fail('invalid-timeout')
  const limits = { ...DEFAULT_LIMITS, ...overrides }
  for (const [key, value] of Object.entries(limits)) {
    if (!Object.hasOwn(DEFAULT_LIMITS, key) || !Number.isSafeInteger(value) || value < 1 || value > DEFAULT_LIMITS[key]) fail('invalid-limit')
  }
  const name = `mnema-crypto-v1:${JSON.stringify([scope.communityId, scope.accountId, scope.deviceId])}`
  const releaseOwner = await acquireOwner(locks, name)
  let db
  try { db = await openDatabase(indexedDB, name) } catch (error) { await releaseOwner(); throw error }
  const active = new Map() // NEVER persist readiness; reopening always requires fresh authorization.
  const transactions = new Set()
  const providerCalls = new Set()
  let closed = false
  let closePromise
  let tail = Promise.resolve()
  let queued = 0

  function enqueue(operation) {
    if (closed) return Promise.reject(new CryptoStateError('store-closed'))
    if (queued >= limits.queuedOperations) return Promise.reject(new CryptoStateError('operation-queue-full'))
    queued++
    const result = tail.then(() => {
      if (closed) fail('store-closed')
      return operation()
    }).finally(() => { queued-- })
    tail = result.catch(() => {})
    return result
  }

  async function transaction(stores, write, operation) {
    let tx
    try {
      tx = db.transaction(stores, write ? 'readwrite' : 'readonly', write ? { durability: 'strict' } : undefined)
    } catch {
      active.clear()
      fail('storage-transaction-unavailable')
    }
    transactions.add(tx)
    const completion = new Promise((resolve, reject) => {
      tx.oncomplete = resolve
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
      active.clear() // Corruption/uncertain persistence invalidates all live provider contexts.
      throw error
    } finally { transactions.delete(tx) }
  }

  function validateGroup(record) {
    if (record === undefined) return undefined
    if (record?.schemaVersion !== SCHEMA_VERSION) fail('invalid-stored-state')
    id(record.groupId); id(record.contextId); revision(record.revision)
    bytes(record.state, limits.stateBytes)
    return record
  }
  function validateEvent(record) {
    if (record?.schemaVersion !== SCHEMA_VERSION) fail('invalid-stored-event')
    id(record.groupId); id(record.contextId); id(record.eventId); revision(record.revision)
    bytes(record.ciphertext, limits.ciphertextBytes)
    return record
  }
  function validateUsage(usage) {
    for (const key of ['stateBytes', 'eventBytes', 'eventCount', 'groupCount']) {
      if (!Number.isSafeInteger(usage?.[key]) || usage[key] < 0) fail('invalid-stored-usage')
    }
    if (usage.stateBytes > limits.totalStateBytes || usage.eventBytes > limits.outboxBytes || usage.eventCount > limits.outboxEvents || usage.groupCount > limits.groups) fail('storage-budget-exceeded')
    return usage
  }
  async function readGroup(tx, groupId) {
    return validateGroup(await requestResult(tx.objectStore('groups').get(groupId)))
  }
  async function readUsage(tx) {
    return validateUsage(await requestResult(tx.objectStore('usage').get('totals')))
  }
  function assertCurrent(record, expectedRevision, contextId) {
    if (!record || record.revision !== expectedRevision || record.contextId !== contextId) fail('state-conflict')
    if (active.get(record.groupId) !== contextId) fail('fresh-context-required')
  }
  function writeState(tx, usage, record, state, contextId, groupId) {
    usage.stateBytes += state.byteLength - (record?.state.byteLength || 0)
    if (!record) usage.groupCount++
    validateUsage(usage)
    const next = { schemaVersion: SCHEMA_VERSION, groupId, contextId, revision: (record?.revision || 0) + 1, state }
    revision(next.revision)
    tx.objectStore('groups').put(next)
    return next
  }

  function close() {
    if (closePromise) return closePromise
    closed = true
    active.clear()
    for (const controller of providerCalls) controller.abort()
    for (const tx of transactions) { try { tx.abort() } catch { /* Already complete. */ } }
    db.close()
    // Retain the lock until pending writes and queued work have settled.
    closePromise = tail.then(releaseOwner)
    return closePromise
  }
  db.onversionchange = () => { void close() }
  db.onclose = () => { void close() }

  async function freshContext(groupId, previous) {
    const controller = new AbortController()
    providerCalls.add(controller)
    let timer
    try {
      return await Promise.race([
        Promise.resolve().then(() => establishFreshContext({ namespace: scope, groupId, previous, signal: controller.signal })),
        new Promise((_, reject) => {
          controller.signal.addEventListener('abort', () => reject(new CryptoStateError('context-establishment-aborted')), { once: true })
          timer = setTimeout(() => controller.abort(), contextTimeoutMs)
        })
      ])
    } catch (error) {
      controller.abort()
      throw error
    } finally {
      clearTimeout(timer)
      providerCalls.delete(controller)
    }
  }

  return Object.freeze({
    close,
    namespace: scope,
    loadState(groupId) {
      try { id(groupId) } catch (error) { return Promise.reject(error) }
      return enqueue(() => transaction(['groups'], false, tx => readGroup(tx, groupId)))
    },
    establishContext({ groupId, expectedRevision }) {
      return enqueue(async () => {
        active.delete(groupId)
        id(groupId); revision(expectedRevision)
        const previous = await transaction(['groups'], false, tx => readGroup(tx, groupId))
        if ((previous?.revision || 0) !== expectedRevision) fail('state-conflict')
        // Crypto/network work MUST happen outside the IDB transaction.
        const fresh = await freshContext(groupId, previous)
        const contextId = id(fresh?.contextId)
        const state = bytes(fresh?.state, limits.stateBytes)
        if (previous?.contextId === contextId) fail('context-reuse')
        if (closed) fail('store-closed')
        const saved = await transaction(['groups', 'usage'], true, async tx => {
          const current = await readGroup(tx, groupId)
          if ((current?.revision || 0) !== expectedRevision) fail('state-conflict')
          const usage = await readUsage(tx)
          const next = writeState(tx, usage, current, state, contextId, groupId)
          tx.objectStore('usage').put(usage, 'totals')
          return next
        })
        if (closed) fail('store-closed')
        active.set(groupId, contextId)
        return saved
      })
    },
    commitSend(input) {
      // Copy synchronously so callers cannot mutate bytes while work is queued.
      let send
      try {
        send = { groupId: id(input?.groupId), contextId: id(input?.contextId), expectedRevision: revision(input?.expectedRevision), eventId: id(input?.eventId), state: bytes(input?.nextState, limits.stateBytes), ciphertext: bytes(input?.ciphertext, limits.ciphertextBytes) }
      } catch (error) { active.clear(); return Promise.reject(error) }
      return enqueue(() => transaction(['groups', 'outbox', 'usage'], true, async tx => {
        const current = await readGroup(tx, send.groupId)
        assertCurrent(current, send.expectedRevision, send.contextId)
        if (await requestResult(tx.objectStore('outbox').get([send.groupId, send.eventId]))) fail('event-already-committed')
        const usage = await readUsage(tx)
        usage.eventCount++
        usage.eventBytes += send.ciphertext.byteLength
        const next = writeState(tx, usage, current, send.state, send.contextId, send.groupId)
        const event = { schemaVersion: SCHEMA_VERSION, groupId: send.groupId, contextId: send.contextId, eventId: send.eventId, revision: next.revision, ciphertext: send.ciphertext }
        tx.objectStore('outbox').add(event)
        tx.objectStore('usage').put(usage, 'totals')
        return event
      })).catch(error => { active.clear(); throw error })
    },
    commitState({ groupId, contextId, expectedRevision, nextState }) {
      let state
      try { id(groupId); id(contextId); revision(expectedRevision); state = bytes(nextState, limits.stateBytes) }
      catch (error) { active.clear(); return Promise.reject(error) }
      return enqueue(() => transaction(['groups', 'usage'], true, async tx => {
        const current = await readGroup(tx, groupId)
        assertCurrent(current, expectedRevision, contextId)
        const usage = await readUsage(tx)
        const next = writeState(tx, usage, current, state, contextId, groupId)
        tx.objectStore('usage').put(usage, 'totals')
        return next
      })).catch(error => { active.clear(); throw error })
    },
    listPending(groupId, limit = limits.outboxEvents) {
      try { id(groupId); if (!Number.isSafeInteger(limit) || limit < 1 || limit > limits.outboxEvents) fail('invalid-limit') }
      catch (error) { return Promise.reject(error) }
      return enqueue(() => transaction(['outbox'], false, async tx => {
        const events = await requestResult(tx.objectStore('outbox').index('group').getAll(groupId, limit))
        return events.map(validateEvent)
      }))
    },
    acknowledge({ groupId, eventId, contextId }) {
      try { id(groupId); id(eventId); id(contextId) } catch (error) { return Promise.reject(error) }
      return enqueue(() => transaction(['outbox', 'usage'], true, async tx => {
        const record = await requestResult(tx.objectStore('outbox').get([groupId, eventId]))
        if (!record) return false
        validateEvent(record)
        if (record.contextId !== contextId) fail('acknowledgement-context-conflict')
        const usage = await readUsage(tx)
        usage.eventCount--; usage.eventBytes -= record.ciphertext.byteLength
        validateUsage(usage)
        tx.objectStore('outbox').delete([groupId, eventId])
        tx.objectStore('usage').put(usage, 'totals')
        return true
      }))
    }
  })
}
