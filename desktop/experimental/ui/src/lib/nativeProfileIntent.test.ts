import { beforeEach, expect, it, vi } from 'vitest'
import { captureNativeChatPresentation, captureNativeTrustPresentation, connectNative, disconnectNative, initializeNativeContext, installNativePort, nativeApiTransport } from './nativeTransport'
import type { NativePort } from './nativeTransport'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const FIRST = '00000000-0000-4000-8000-000000000002'
const SECOND = '00000000-0000-4000-8000-000000000003'
let selected: string | null
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
const descriptor = () => ({ context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: selected })
const connected = (profile: string) => ({ context: CONTEXT, status: 200, body: { origin: 'https://example.invalid', community_id: 'synthetic', compatibility: 'supported', transport_preview: true, content_authorization: 'unavailable', profile_intent: profile } })
beforeEach(async () => {
  selected = FIRST
  invoke = vi.fn(async command => {
    if (command === 'native_context') return descriptor()
    if (command === 'native_connect') { selected = SECOND; return connected(SECOND) }
    return { context: CONTEXT, status: 204, body: null }
  })
  installNativePort({ invoke, channel: () => ({ onmessage() {} }), trustChannel: () => ({ onmessage() {} }), chatChannel: () => ({ onmessage() {} }) }); await initializeNativeContext()
})
it('captures the old native comparator in delayed login even after a new connection selects another origin', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const old = nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'synthetic', password: 'synthetic old password' } })
  await connectNative('second.example.invalid')
  const login = invoke.mock.calls.find(call => call[0] === 'native_auth_login')
  expect(login?.[1]?.profileIntent).toBe(FIRST)
  expect(invoke.mock.calls.find(call => call[0] === 'native_connect')?.[1]?.profileIntent).toBe(FIRST)
  finish({ context: CONTEXT, status: 200, body: {} }); await expect(old).rejects.toMatchObject({ name: 'AbortError' })
  await nativeApiTransport('/api/auth/me', { method: 'GET' })
  expect(invoke).toHaveBeenLastCalledWith('native_auth_me', expect.objectContaining({ profileIntent: SECOND }))
})
it('has no profile-bound credential or content entrypoint before a native profile is selected', async () => {
  selected = null; await initializeNativeContext(); const calls = invoke.mock.calls.length
  await expect(nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'synthetic', password: 'synthetic' } })).rejects.toThrow('profile unavailable')
  expect(() => captureNativeTrustPresentation()).toThrow('unavailable')
  expect(() => captureNativeChatPresentation(FIRST, CONTEXT)).toThrow('unavailable')
  expect(invoke.mock.calls).toHaveLength(calls)
})
it('recovers the public current comparator after lost Connect acknowledgement for an explicit new connection only', async () => {
  invoke.mockImplementationOnce(async () => { selected = SECOND; throw new Error('synthetic lost Connect acknowledgement') })
  await expect(connectNative('second.example.invalid')).rejects.toThrow()
  await expect(nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'synthetic', password: 'synthetic' } })).rejects.toThrow('profile unavailable')
  await connectNative('second.example.invalid')
  const connects = invoke.mock.calls.filter(call => call[0] === 'native_connect')
  expect(connects).toHaveLength(2); expect(connects[0]?.[1]?.profileIntent).toBe(FIRST); expect(connects[1]?.[1]?.profileIntent).toBe(SECOND)
  expect(invoke.mock.calls.some(call => call[0] === 'native_auth_login')).toBe(false)
})
it('captures old disconnect comparator and does not overwrite a subsequently selected profile on late ACK', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const old = disconnectNative(); await connectNative('second.example.invalid')
  expect(invoke.mock.calls.find(call => call[0] === 'native_disconnect')?.[1]?.profileIntent).toBe(FIRST)
  finish({ context: CONTEXT, status: 204, body: null }); await old
  await nativeApiTransport('/api/auth/me', { method: 'GET' }); expect(invoke).toHaveBeenLastCalledWith('native_auth_me', expect.objectContaining({ profileIntent: SECOND }))
})
it('fails closed on invalid native intent descriptors and Connect acknowledgements', async () => {
  for (const profile of [true, '', '00000000-0000-0000-0000-000000000000']) {
    invoke.mockResolvedValueOnce({ ...descriptor(), profile_intent: profile }); await expect(initializeNativeContext()).rejects.toThrow()
  }
  invoke.mockResolvedValueOnce({ context: CONTEXT, status: 200, body: { profile_intent: 'invalid' } })
  await expect(connectNative('example.invalid')).rejects.toThrow()
  await expect(nativeApiTransport('/api/auth/me', { method: 'GET' })).rejects.toThrow('profile unavailable')
})
