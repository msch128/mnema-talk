import { beforeEach, expect, it, vi } from 'vitest'
import { captureNativeChatPresentation, connectNative, disconnectNative, initializeNativeContext, installNativePort, nativeApiTransport } from './nativeTransport'
import type { NativePort } from './nativeTransport'
import type { StatusChannel } from './trustPort'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const CHANNEL = '00000000-0000-4000-8000-000000000002'
const EVENT = '00000000-0000-4000-8000-000000000003'
let channel: StatusChannel
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
const row = { id: EVENT, number: 1, channel_id: CHANNEL, client_event_id: EVENT, account_id: CONTEXT, device_id: EVENT, body: 'session cached native projection' }
const ack = { context: CONTEXT, status: 200, body: { state: 'completed' } }
beforeEach(async () => {
  channel = { onmessage() {} }
  invoke = vi.fn(async command => {
    if (command === 'native_context') return { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: CONTEXT }
    if (command === 'native_chat_receive') { channel.onmessage([row]); return ack }
    return { context: CONTEXT, status: 204, body: null }
  })
  installNativePort({ invoke, channel: () => ({ onmessage() {} }), chatChannel: () => { channel = { onmessage() {} }; return channel } }); await initializeNativeContext()
})
it('keeps only acknowledged verified projections for channel remount and clones all presentation returns', async () => {
  const port = captureNativeChatPresentation(CHANNEL, CONTEXT); const result = await port.receive()
  if (result[0]) result[0].body = 'caller mutation'
  expect(port.snapshot()[0]?.body).toBe(row.body); port.dispose()
  expect(captureNativeChatPresentation(CHANNEL, CONTEXT).snapshot()[0]?.body).toBe(row.body)
  expect(captureNativeChatPresentation(EVENT, CONTEXT).snapshot()).toEqual([])
  expect(captureNativeChatPresentation(CHANNEL, EVENT).snapshot()).toEqual([])
})
it('clears cached rows before failed login/logout or profile transitions start awaiting', async () => {
  for (const [index, change] of [
    () => nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'synthetic', password: 'synthetic' } }),
    () => nativeApiTransport('/api/auth/logout', { method: 'POST' }),
    () => connectNative('example.invalid'),
    () => disconnectNative(),
  ].entries()) {
    await initializeNativeContext()
    const old = captureNativeChatPresentation(CHANNEL, CONTEXT); await old.receive()
    let fail!: (reason: unknown) => void; invoke.mockImplementationOnce(() => new Promise((_resolve, reject) => { fail = reject }))
    const changing = change();
    if (index < 2) expect(captureNativeChatPresentation(CHANNEL, CONTEXT).snapshot()).toEqual([])
    else expect(() => captureNativeChatPresentation(CHANNEL, CONTEXT)).toThrow('unavailable')
    expect(() => old.snapshot()).toThrow()
    fail(new Error('synthetic transition failure')); await expect(changing).rejects.toThrow()
    await initializeNativeContext()
    expect(captureNativeChatPresentation(CHANNEL, CONTEXT).snapshot()).toEqual([])
  }
})
it('does not persist projections after failed ACK or disposed late scope', async () => {
  const port = captureNativeChatPresentation(CHANNEL, CONTEXT)
  invoke.mockImplementationOnce(async () => { channel.onmessage([row]); return { ...ack, status: 503 } })
  await expect(port.receive()).rejects.toThrow(); expect(port.snapshot()).toEqual([])
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; channel.onmessage([row]) }))
  const pending = port.receive(); port.dispose(); finish(ack)
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  expect(captureNativeChatPresentation(CHANNEL, CONTEXT).snapshot()).toEqual([])
})
