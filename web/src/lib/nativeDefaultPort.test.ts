import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { StatusChannel } from './trustPort'
const context = '10000000-0000-4000-8000-000000000001'
const channelId = '20000000-0000-4000-8000-000000000001'
const operationId = '30000000-0000-4000-8000-000000000001'
afterEach(() => vi.unstubAllGlobals())
it('uses the actual pinned Tauri API factories for default socket, trust and typed-chat commands', async () => {
  vi.resetModules()
  let next = 0
  const callbacks = new Map<number, (value: unknown) => void>()
  const invoke = vi.fn(async (command: string, args?: Record<string, unknown>) => {
    if (command === 'native_context') return { context, profile_intent: context, authentication_intent: context, content_authorization: 'unavailable', remembered_login: false }
    if (command === 'native_trust_begin_first_root') return { context, status: 200, body: { version: 1, operation_id: operationId,
      operation_kind: 'first_root', expires_at: '2026-10-08T16:00:00Z', scope: { origin: 'https://community.example.invalid', community_id: 'synthetic', channel_id: channelId, account_id: context, device_id: operationId, group_id: 'YWJj' },
      root_fingerprint_hex: 'a'.repeat(64), root_public_key_hex: 'b'.repeat(64), device_public_key_hex: 'c'.repeat(64) } }
    if (command === 'native_chat_snapshot') {
      (args?.['onMessages'] as StatusChannel).onmessage({ channel_id: channelId, messages: [] })
      return { context, status: 200, body: { state: 'completed' } }
    }
    return { context, status: command === 'native_socket_open' ? 503 : 204, body: null }
  })
  vi.stubGlobal('__TAURI_INTERNALS__', { invoke,
    transformCallback(callback: (value: unknown) => void) { callbacks.set(++next, callback); return next },
    unregisterCallback(id: number) { callbacks.delete(id) } })
  const native = await import('./nativeTransport')
  await native.initializeNativeContext()
  const socket = new native.NativeSocket(); await flushPromises()
  expect(socket.readyState).toBe(3)
  const trust = native.captureNativeTrustPresentation()
  expect(await trust.beginFirstRoot(channelId)).toMatchObject({ operation_id: operationId })
  expect(await native.nativeApiTransport(`/api/channels/${channelId}/messages`, { method: 'GET' })).toEqual({ status: 200, body: [] })
  expect(next).toBe(3)
  expect(invoke.mock.calls.map(c => c[0])).toContain('native_chat_snapshot')
  await trust.cancel(operationId)
  await native.disconnectNative()
})
