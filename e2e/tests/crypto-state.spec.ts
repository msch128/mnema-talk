import ts from '../../web/node_modules/typescript/lib/typescript.js'
import { createServer, type Server } from 'node:http'
import { readFile } from 'node:fs/promises'
import { test, expect, type Page } from '@playwright/test'

// Tests the persistence boundary in actual Chromium. These synthetic opaque
// provider states are not an MLS implementation or an E2EE integration proof.
let server: Server
let origin: string
test.beforeAll(async () => {
  const sourceText = await readFile(new URL('../../web/src/lib/crypto/stateStore.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(sourceText, { fileName: 'stateStore.ts', compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }, reportDiagnostics: true })
  if (compiled.diagnostics?.some(diagnostic => diagnostic.category === ts.DiagnosticCategory.Error)) throw new Error('Crypto persistence fixture compilation failed')
  const source = compiled.outputText
  server = createServer((request, response) => {
    if (request.url === '/') {
      response.writeHead(200, { 'Content-Type': 'text/html', 'Cache-Control': 'no-store' })
      response.end('<!doctype html><title>Disposable crypto persistence test</title>')
    } else if (request.url === '/stateStore.js') {
      response.writeHead(200, { 'Content-Type': 'text/javascript', 'Cache-Control': 'no-store' })
      response.end(source)
    } else {
      response.writeHead(404)
      response.end()
    }
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('Expected disposable TCP server address')
  origin = `http://127.0.0.1:${address.port}`
})
test.afterAll(async () => { await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())) })

async function connect(page: Page, contextId: string) {
  await page.goto(origin)
  return page.evaluate(async contextId => {
    const moduleUrl = '/stateStore.js'
    const { openCryptoStateStore }: typeof import('../../web/src/lib/crypto/stateStore') = await import(moduleUrl)
    window.openStore = fresh => openCryptoStateStore({
      namespace: { communityId: 'synthetic-community', accountId: 'synthetic-account', deviceId: 'synthetic-device' },
      establishFreshContext: async () => ({ contextId: fresh, state: new Uint8Array([1]) })
    })
    try { window.store = await window.openStore(contextId); return 'owned' }
    catch (error) { if (!error || typeof error !== 'object' || !('code' in error) || typeof error.code !== 'string') throw error
      return error.code }
  }, contextId)
}

test('native Web Lock prevents parallel ownership and a crashed tab cannot resume sender state', async ({ context }) => {
  const owner = await context.newPage()
  const other = await context.newPage()
  expect(await connect(owner, 'synthetic-context-1')).toBe('owned')
  expect(await connect(other, 'synthetic-context-2')).toBe('owned-by-another-tab')
  const committed = await owner.evaluate(async () => {
    const state = await window.store.establishContext({ groupId: 'group', expectedRevision: 0 })
    const event = await window.store.commitSend({ groupId: 'group', contextId: state.contextId, expectedRevision: state.revision, eventId: 'stable-event', nextState: new Uint8Array([2]), ciphertext: new Uint8Array([7, 8]) })
    return { revision: event.revision, bytes: [...event.ciphertext] }
  })
  expect(committed).toEqual({ revision: 2, bytes: [7, 8] })
  const session = await context.newCDPSession(owner)
  const crashed = owner.waitForEvent('crash')
  void session.send('Page.crash').catch(() => {})
  await crashed
  await owner.close()
  await expect.poll(async () => other.evaluate(async () => {
    try { window.store = await window.openStore('synthetic-context-2'); return 'owned' }
    catch (error) { if (!error || typeof error !== 'object' || !('code' in error) || typeof error.code !== 'string') throw error
      return error.code }
  })).toBe('owned')
  const restored = await other.evaluate(async () => {
    const state = await window.store.loadState('group')
    if (!state) throw new Error('Expected committed state')
    let blocked
    try {
      await window.store.commitSend({ groupId: 'group', contextId: state.contextId, expectedRevision: state.revision, eventId: 'new-event', nextState: new Uint8Array([3]), ciphertext: new Uint8Array([9]) })
    } catch (error) { if (!error || typeof error !== 'object' || !('code' in error) || typeof error.code !== 'string') throw error
      blocked = error.code }
    const pending = await window.store.listPending('group')
    const fresh = await window.store.establishContext({ groupId: 'group', expectedRevision: state.revision })
    await window.store.close()
    return { blocked, pending: pending.map(event => [...event.ciphertext]), contextId: fresh.contextId, revision: fresh.revision }
  })
  expect(restored).toEqual({ blocked: 'fresh-context-required', pending: [[7, 8]], contextId: 'synthetic-context-2', revision: 3 })
})

test('native IndexedDB abort leaves neither new sender state nor outgoing ciphertext', async ({ page }) => {
  expect(await connect(page, 'synthetic-context')).toBe('owned')
  const result = await page.evaluate(async () => {
    const state = await window.store.establishContext({ groupId: 'group', expectedRevision: 0 })
    const original = IDBObjectStore.prototype.add
    IDBObjectStore.prototype.add = function (...args) {
      if (this.name === 'outbox') { this.transaction.abort(); throw new Error('synthetic write interruption') }
      return original.apply(this, args)
    }
    let rejected = false
    try {
      await window.store.commitSend({ groupId: 'group', contextId: state.contextId, expectedRevision: 1, eventId: 'stable-event', nextState: new Uint8Array([2]), ciphertext: new Uint8Array([7, 8]) })
    } catch { rejected = true }
    finally { IDBObjectStore.prototype.add = original }
    const saved = await window.store.loadState('group')
    if (!saved) throw new Error('Expected committed state')
    const pending = await window.store.listPending('group')
    await window.store.close()
    return { rejected, revision: saved.revision, state: [...saved.state], pending: pending.length }
  })
  expect(result).toEqual({ rejected: true, revision: 1, state: [1], pending: 0 })
})
