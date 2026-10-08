import { beforeEach, expect, it, vi } from 'vitest'
import { captureNativeTrustPresentation, connectNative, disconnectNative, initializeNativeContext, installNativePort, nativeApiTransport } from './nativeTransport'
import type { NativePort } from './nativeTransport'
import type { StatusChannel } from './trustPort'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const CHANNEL = '00000000-0000-4000-8000-000000000002'
const OPERATION = '00000000-0000-4000-8000-000000000003'
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
let channel: StatusChannel
const preview = { version: 1, operation_id: OPERATION, operation_kind: 'first_root', expires_at: '2026-10-08T16:00:00Z', scope: { origin: 'https://example.invalid', community_id: 'synthetic-community', channel_id: CHANNEL, account_id: CONTEXT, device_id: OPERATION, group_id: 'YWJj' }, root_fingerprint_hex: 'a'.repeat(64), root_public_key_hex: 'b'.repeat(64), device_public_key_hex: 'c'.repeat(64) }
beforeEach(async () => {
  channel = { onmessage() {} }
  invoke = vi.fn(async command => command === 'native_context' ? { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: CONTEXT } : { context: CONTEXT, status: command === 'native_disconnect' ? 204 : 200, body: command === 'native_connect' ? { profile_intent: CONTEXT } : command === 'native_trust_begin_first_root' ? preview : { operation_id: OPERATION, state: command === 'native_trust_cancel' ? 'cancelled' : 'pending' } })
  installNativePort({ invoke, channel: () => ({ onmessage() {} }), trustChannel: () => channel })
  await initializeNativeContext()
})
it('captures the real injectable Channel and forwards only four fixed public trust arguments', async () => {
  const port = captureNativeTrustPresentation(); await port.beginFirstRoot(CHANNEL)
  expect(invoke).toHaveBeenLastCalledWith('native_trust_begin_first_root', { context: CONTEXT, channelId: CHANNEL, onStatus: channel, profileIntent: CONTEXT })
  await port.requestConfirmation(OPERATION)
  expect(invoke).toHaveBeenLastCalledWith('native_trust_request_confirmation', { context: CONTEXT, operationId: OPERATION, profileIntent: CONTEXT })
  await port.cancel(OPERATION)
  expect(invoke).toHaveBeenLastCalledWith('native_trust_cancel', { context: CONTEXT, operationId: OPERATION, profileIntent: CONTEXT })
})
it('retires captured presentation before replacement login or logout even if account/context stays identical', async () => {
  for (const [path, request] of [['/api/auth/login', { method: 'POST', json: { username: 'synthetic', password: 'synthetic' } }], ['/api/auth/logout', { method: 'POST' }]] as const) {
    const old = captureNativeTrustPresentation()
    await nativeApiTransport(path, request)
    const calls = invoke.mock.calls.length
    await expect(old.beginFirstRoot(CHANNEL)).rejects.toMatchObject({ name: 'AbortError' })
    expect(invoke.mock.calls).toHaveLength(calls)
  }
})
it('retires on connect/disconnect and replacement port regardless of reused document UUID', async () => {
  for (const change of [() => connectNative('example.invalid'), () => disconnectNative(), async () => { installNativePort({ invoke, channel: () => ({ onmessage() {} }), trustChannel: () => channel }); await initializeNativeContext() }]) {
    await initializeNativeContext()
    const old = captureNativeTrustPresentation(); await change(); const calls = invoke.mock.calls.length
    await expect(old.beginFirstRoot(CHANNEL)).rejects.toMatchObject({ name: 'AbortError' })
    expect(invoke.mock.calls).toHaveLength(calls)
  }
})
it('cancels only its own delayed prepared operation after native logout', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(r => { finish = r }))
  const port = captureNativeTrustPresentation(); const pending = port.beginFirstRoot(CHANNEL)
  await nativeApiTransport('/api/auth/logout', { method: 'POST' })
  finish({ context: CONTEXT, status: 200, body: preview })
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  expect(invoke).toHaveBeenLastCalledWith('native_trust_cancel', { context: CONTEXT, operationId: OPERATION, profileIntent: CONTEXT })
})
