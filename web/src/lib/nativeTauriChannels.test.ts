import { Channel } from '@tauri-apps/api/core'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createTrustPort } from './trustPort'
import { initializeNativeContext, installNativePort, NativeSocket } from './nativeTransport'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const CHANNEL = '00000000-0000-4000-8000-000000000002'
const OPERATION = '00000000-0000-4000-8000-000000000003'
const SECOND = '00000000-0000-4000-8000-000000000004'
const preview = { version: 1, operation_id: OPERATION, operation_kind: 'first_root', expires_at: '2026-10-08T16:00:00Z', scope: { origin: 'https://example.invalid', community_id: 'synthetic-community', channel_id: CHANNEL, account_id: CONTEXT, device_id: SECOND, group_id: 'YWJj' }, root_fingerprint_hex: 'a'.repeat(64), root_public_key_hex: 'b'.repeat(64), device_public_key_hex: 'c'.repeat(64) }
let callbacks: Map<number, (value: unknown) => void>
beforeEach(() => {
  callbacks = new Map(); let next = 0
  // Actual pinned Channel implementation. Instrument only its documented
  // callback registration boundary, without reimplementing message ordering.
  vi.stubGlobal('__TAURI_INTERNALS__', {
    transformCallback(callback: (value: unknown) => void) { const id = ++next; callbacks.set(id, callback); return id },
    unregisterCallback(id: number) { callbacks.delete(id) },
  })
})
afterEach(() => { vi.unstubAllGlobals() })
it('subscribing and closing unprepared trust presentation allocates no actual Tauri callbacks', () => {
  for (let index = 0; index < 5; index++) {
    const port = createTrustPort({ context: CONTEXT, isCurrent: () => true, invoke: async () => { throw new Error('unexpected native call') }, channel: () => new Channel<unknown>() })
    const unsubscribe = port.subscribe(() => {}); unsubscribe()
  }
  expect(callbacks.size).toBe(0)
})
it('creates a fresh callback after actual native end and ignores malformed old-attempt messages', async () => {
  const channels: Channel<unknown>[] = []
  const port = createTrustPort({ context: CONTEXT, isCurrent: () => true,
    channel: () => { const channel = new Channel<unknown>(); channels.push(channel); return channel },
    invoke: async () => ({ context: CONTEXT, status: 200, body: { ...preview, operation_id: channels.length === 1 ? OPERATION : SECOND } })
  })
  const received: unknown[] = []; port.subscribe(value => received.push(value))
  await port.beginFirstRoot(CHANNEL); const first = channels[0]; expect(first).toBeDefined()
  callbacks.get(first!.id)?.({ index: 0, message: { operation_id: OPERATION, state: 'cancelled' } })
  callbacks.get(first!.id)?.({ index: 1, end: true }); expect(callbacks.has(first!.id)).toBe(false)
  await port.beginFirstRoot(CHANNEL); const second = channels[1]; expect(second).toBeDefined(); expect(second!.id).not.toBe(first!.id)
  first!.onmessage({ malformed: true })
  callbacks.get(second!.id)?.({ index: 0, message: { operation_id: SECOND, state: 'root_saved' } })
  expect(received.at(-1)).toEqual({ operation_id: SECOND, state: 'root_saved' })
  callbacks.get(second!.id)?.({ index: 1, end: true }); expect(callbacks.size).toBe(0)
})
it('does not register a socket callback when native context has no selected profile', async () => {
  const invoke = vi.fn(async () => ({ context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, authentication_intent: null, profile_intent: null }))
  installNativePort({ invoke, channel: () => new Channel(), trustChannel: () => new Channel<unknown>() })
  await initializeNativeContext(); const socket = new NativeSocket(); await Promise.resolve()
  expect(callbacks.size).toBe(0); expect(socket.readyState).toBe(3)
  expect(invoke).toHaveBeenCalledTimes(1)
})
it('does not allocate actual Tauri callbacks for a selected but unauthenticated profile', async () => {
  const invoke = vi.fn(async () => ({ context: CONTEXT, profile_intent: CONTEXT, authentication_intent: null, content_authorization: 'unavailable', remembered_login: false }))
  installNativePort({ invoke, channel: () => new Channel(), trustChannel: () => new Channel<unknown>() })
  await initializeNativeContext()
  const sockets = Array.from({ length: 5 }, () => new NativeSocket())
  await Promise.resolve()
  expect(callbacks.size).toBe(0)
  expect(sockets.every(socket => socket.readyState === 3)).toBe(true)
  expect(invoke).toHaveBeenCalledTimes(1)
})
