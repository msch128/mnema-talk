import { it } from 'vitest'
import assert from 'node:assert/strict'
import { createTrustPort, type CapturedTrustBridge, type StatusChannel } from './trustPort.ts'
const context = '00000000-0000-4000-8000-000000000001'
const channelId = '00000000-0000-4000-8000-000000000002'
const operationId = '00000000-0000-4000-8000-000000000003'
const other = '00000000-0000-4000-8000-000000000004'
const preview = { version: 1, operation_id: operationId, operation_kind: 'first_root', expires_at: '2026-10-08T16:00:00Z', scope: { origin: 'https://example.invalid', community_id: 'synthetic-community', channel_id: channelId, account_id: context, device_id: other, group_id: 'YWJj' }, root_fingerprint_hex: 'a'.repeat(64), root_public_key_hex: 'b'.repeat(64), device_public_key_hex: 'c'.repeat(64) }
const status = (state: string, id = operationId) => ({ operation_id: id, state })
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { resolve, promise } }
function harness() {
  let current = true
  const calls: { command: string; args: Record<string, unknown> }[] = []
  const channel: StatusChannel = { onmessage() {} }
  let handle: (command: string) => Promise<unknown> = async command => ({ context, status: 200, body: command === 'native_trust_begin_first_root' ? preview : status(command === 'native_trust_cancel' ? 'cancelled' : 'pending') })
  const bridge: CapturedTrustBridge = { context, isCurrent: () => current, channel: () => channel, async invoke(command, args) { calls.push({ command, args }); return handle(command) } }
  return { port: createTrustPort(bridge), calls, channel, retire() { current = false }, setHandler(fn: typeof handle) { handle = fn } }
}
it('replays pre-ack terminal status after awaited preview ownership, and never reverses terminal state', async () => {
  const h = harness(); const pending = deferred<unknown>(); h.setHandler(() => pending.promise)
  let ownsPreview = false; const updates: unknown[] = []
  h.port.subscribe(value => { assert.equal(ownsPreview, true); updates.push(value) })
  const begin = (async () => { const result = await h.port.beginFirstRoot(channelId); ownsPreview = true; return result })()
  h.channel.onmessage(status('pending')); h.channel.onmessage(status('root_saved'))
  pending.resolve({ context, status: 200, body: preview }); await begin; await Promise.resolve()
  assert.deepEqual(updates, [status('pending'), status('root_saved')])
  h.channel.onmessage(status('pending')); h.channel.onmessage(status('root_saved', other))
  assert.equal(updates.length, 2)
  await assert.rejects(h.port.requestConfirmation(operationId))
})
it('only requests the fixed native OS command; terminal callback wins over late pending acknowledgment', async () => {
  const h = harness(); await h.port.beginFirstRoot(channelId)
  const pending = deferred<unknown>(); h.setHandler(() => pending.promise)
  const updates: unknown[] = []; h.port.subscribe(value => updates.push(value))
  const request = h.port.requestConfirmation(operationId)
  h.channel.onmessage(status('root_saved')); pending.resolve({ context, status: 200, body: status('pending') })
  assert.deepEqual(await request, status('pending')); assert.deepEqual(updates, [status('root_saved')])
  assert.deepEqual(h.calls[1], { command: 'native_trust_request_confirmation', args: { context, operationId } })
})
it('cancels only the old operation after a late begin reply for a retired document', async () => {
  const h = harness(); const pending = deferred<unknown>()
  h.setHandler(command => command === 'native_trust_begin_first_root' ? pending.promise : Promise.resolve({ context, status: 200, body: status('cancelled') }))
  const begin = h.port.beginFirstRoot(channelId); h.retire(); pending.resolve({ context, status: 200, body: preview })
  await assert.rejects(begin, { name: 'AbortError' })
  assert.deepEqual(h.calls[1], { command: 'native_trust_cancel', args: { context, operationId } })
  assert.equal(h.calls.length, 2)
})
it('rejects foreign channel previews and malformed or oversized early status streams', async () => {
  const wrong = harness(); wrong.setHandler(async command => ({ context, status: 200, body: command === 'native_trust_begin_first_root' ? { ...preview, scope: { ...preview.scope, channel_id: other } } : status('cancelled') }))
  await assert.rejects(wrong.port.beginFirstRoot(channelId)); assert.equal(wrong.calls[1]?.command, 'native_trust_cancel')
  for (const malformed of [true, false]) {
    const h = harness(); const pending = deferred<unknown>()
    h.setHandler(command => command === 'native_trust_begin_first_root' ? pending.promise : Promise.resolve({ context, status: 200, body: status('cancelled') }))
    const begin = h.port.beginFirstRoot(channelId)
    if (malformed) h.channel.onmessage({ ...status('pending'), private_key: 'forbidden' })
    else for (let n = 0; n < 33; n++) h.channel.onmessage(status('pending'))
    pending.resolve({ context, status: 200, body: preview }); await assert.rejects(begin)
    assert.equal(h.calls[1]?.command, 'native_trust_cancel')
  }
})
it('allows a fresh setup after native cancellation but never after root adoption', async () => {
  const h = harness(); await h.port.beginFirstRoot(channelId); await h.port.cancel(operationId)
  await h.port.beginFirstRoot(channelId); h.channel.onmessage(status('root_saved'))
  h.setHandler(async () => ({ context, status: 200, body: status('root_saved') }))
  await h.port.cancel(operationId)
  await assert.rejects(h.port.beginFirstRoot(channelId))
  assert.equal(h.calls.filter(c => c.command === 'native_trust_begin_first_root').length, 2)
})
it('rejects wrong contexts, extra reply fields and invalid acknowledgments without publishing success', async () => {
  for (const value of [{ context: other, status: 200, body: preview }, { context, status: 200, body: preview, ready: true }, { context, status: 999, body: preview }]) {
    const h = harness(); h.setHandler(async () => value); await assert.rejects(h.port.beginFirstRoot(channelId))
  }
  const h = harness(); await h.port.beginFirstRoot(channelId)
  h.setHandler(async () => ({ context, status: 200, body: status('root_saved') }))
  await assert.rejects(h.port.requestConfirmation(operationId))
  await assert.rejects(h.port.cancel(other))
})
it('fixed read-status ignores stale completion and subscription disposal suppresses later status', async () => {
  const h = harness(); await h.port.beginFirstRoot(channelId)
  const updates: unknown[] = []; const unsubscribe = h.port.subscribe(v => updates.push(v)); unsubscribe()
  h.channel.onmessage(status('pending')); assert.equal(updates.length, 0)
  const pending = deferred<unknown>(); h.setHandler(() => pending.promise)
  const read = h.port.readCurrentStatus(); h.retire(); pending.resolve({ context, status: 200, body: status('root_saved') })
  await assert.rejects(read, { name: 'AbortError' })
  assert.deepEqual(h.calls[1], { command: 'native_trust_read_status', args: { context } })
})
it('fails closed on invalid context and on unavailable, malformed or foreign read-status replies', async () => {
  assert.throws(() => createTrustPort({ context: 'invalid', isCurrent: () => true, invoke: async () => null, channel: () => ({ onmessage() {} }) }))
  for (const value of [null, [], { context, status: 99, body: null }, { context, status: 200.5, body: null },
    { context, status: 503, body: null }, { context, status: 200, body: status('pending', other) }]) {
    const h = harness(); await h.port.beginFirstRoot(channelId); h.setHandler(async () => value)
    await assert.rejects(h.port.readCurrentStatus())
  }
  const empty = harness(); empty.setHandler(async () => ({ context, status: 204, body: null }))
  assert.equal(await empty.port.readCurrentStatus(), null)
  const h = harness(); await h.port.beginFirstRoot(channelId)
  const updates: unknown[] = []; h.port.subscribe(value => updates.push(value))
  h.setHandler(async () => ({ context, status: 200, body: status('root_saved') }))
  assert.deepEqual(await h.port.readCurrentStatus(), status('root_saved'))
  assert.deepEqual(updates, [status('root_saved')])
})
it('rejects unavailable begin and confirmation and handles native no-op cancellation', async () => {
  const h = harness(); h.setHandler(async () => ({ context, status: 503, body: null }))
  await assert.rejects(h.port.beginFirstRoot(channelId))
  h.setHandler(async () => ({ context, status: 200, body: preview })); await h.port.beginFirstRoot(channelId)
  h.setHandler(async () => ({ context, status: 503, body: null }))
  await assert.rejects(h.port.requestConfirmation(operationId)); await assert.rejects(h.port.cancel(operationId))
  h.setHandler(async () => ({ context, status: 200, body: status('pending') }))
  await assert.rejects(h.port.cancel(operationId))
  h.setHandler(async () => ({ context, status: 204, body: null }))
  await h.port.cancel(operationId)
})
it('breaks a live presentation on wrong operation status and ignores later callbacks or failed cleanup', async () => {
  const h = harness(); await h.port.beginFirstRoot(channelId)
  h.setHandler(async () => { throw new Error('Cleanup unavailable') })
  h.channel.onmessage(status('device_saved')); await Promise.resolve(); await Promise.resolve()
  h.channel.onmessage(status('root_saved'))
  await assert.rejects(h.port.readCurrentStatus(), { name: 'AbortError' })
  assert.equal(h.calls.at(-1)?.command, 'native_trust_cancel')
})
it('does not accept status after document retirement', async () => {
  const h = harness(); await h.port.beginFirstRoot(channelId); const updates: unknown[] = []
  h.port.subscribe(value => updates.push(value)); h.retire(); h.channel.onmessage(status('root_saved'))
  assert.deepEqual(updates, [])
})
