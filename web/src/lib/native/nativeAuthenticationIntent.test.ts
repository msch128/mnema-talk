import { beforeEach, expect, it, vi } from 'vitest'
import { captureNativeTrustPresentation, connectNative, initializeNativeContext, installNativePort, nativeApiTransport, NativeSocket } from './nativeTransport'
const CONTEXT = '10000000-0000-4000-8000-000000000001'
const PROFILE = '20000000-0000-4000-8000-000000000001'
const OTHER = '20000000-0000-4000-8000-000000000002'
const FIRST = '50000000-0000-4000-8000-000000000001'
const SECOND = '50000000-0000-4000-8000-000000000002'
const HANDLE = '60000000-0000-4000-8000-000000000001'
let lineage: string | null
let profile: string
const login = { method: 'POST', json: { username: 'synthetic', password: 'synthetic-password' } }
const password = { method: 'PUT', json: { current_password: 'synthetic-old', new_password: 'synthetic-new' } }
const reply = (status: number, body: unknown = null) => ({ context: CONTEXT, status, body })
const invoke = vi.fn(async (command: string, _args?: Record<string, unknown>): Promise<unknown> => {
  if (command === 'native_context') return { context: CONTEXT, profile_intent: profile, authentication_intent: lineage, content_authorization: 'unavailable', remembered_login: false }
  if (command === 'native_auth_login') { lineage = SECOND; return reply(200, { user: { id: CONTEXT }, authentication_intent: SECOND }) }
  if (command === 'native_auth_password') { lineage = SECOND; return reply(200, { authentication_intent: SECOND }) }
  if (command === 'native_connect') { profile = OTHER; lineage = null; return reply(200, { profile_intent: OTHER }) }
  if (command === 'native_socket_open') return reply(200, { handle: HANDLE })
  return reply(204)
})
beforeEach(async () => {
  lineage = FIRST; profile = PROFILE; invoke.mockClear()
  installNativePort({ invoke, channel: () => ({ onmessage: () => {} }), trustChannel: () => ({ onmessage: () => {} }), chatChannel: () => ({ onmessage: () => {} }) })
  await initializeNativeContext(); invoke.mockClear()
})
it('adopts only the accepted login lineage and forwards it on the next authenticated request', async () => {
  await nativeApiTransport('/api/auth/login', login); await nativeApiTransport('/api/auth/me', { method: 'GET' })
  expect(invoke.mock.calls[0]?.[1]?.authenticationIntent).toBe(FIRST)
  expect(invoke.mock.calls[1]?.[1]?.authenticationIntent).toBe(SECOND)
})
it('maps a closed password acknowledgement to web204 while keeping the new native lineage', async () => {
  expect(await nativeApiTransport('/api/auth/password', password)).toEqual({ status: 204, body: null })
  await nativeApiTransport('/api/auth/me', { method: 'GET' })
  expect(invoke.mock.calls[0]?.[1]?.authenticationIntent).toBe(FIRST)
  expect(invoke.mock.calls[1]?.[1]?.authenticationIntent).toBe(SECOND)
})
it('drops an old unauthorized Me response after a same-profile accepted login', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const old = nativeApiTransport('/api/auth/me', { method: 'GET' })
  await nativeApiTransport('/api/auth/login', login); finish(reply(401))
  await expect(old).rejects.toMatchObject({ name: 'AbortError' })
  expect(invoke.mock.calls[0]?.[1]?.authenticationIntent).toBe(FIRST)
})
it('seals trust and chat presentation captures when the same profile accepts a new login', async () => {
  const trust = captureNativeTrustPresentation()
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const chat = nativeApiTransport(`/api/channels/${CONTEXT}/messages`, { method: 'GET' })
  await nativeApiTransport('/api/auth/login', login); const calls = invoke.mock.calls.length
  await expect(trust.beginFirstRoot(CONTEXT)).rejects.toMatchObject({ name: 'AbortError' })
  finish(reply(200, { state: 'completed' }))
  await expect(chat).rejects.toMatchObject({ name: 'AbortError' })
  expect(invoke.mock.calls).toHaveLength(calls)
})
it('targets late socket-handle cleanup at its original auth lineage', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const socket = new NativeSocket(); await nativeApiTransport('/api/auth/login', login)
  finish(reply(200, { handle: HANDLE })); for (let i = 0; i < 5; i++) await Promise.resolve()
  expect(socket.readyState).toBe(3)
  expect(invoke).toHaveBeenCalledWith('native_socket_close', { context: CONTEXT, profileIntent: PROFILE, authenticationIntent: FIRST, handle: HANDLE })
})
it('does not infer lineage after an unknown password ACK; only a new explicit login reads the comparison descriptor', async () => {
  invoke.mockImplementationOnce(async () => { lineage = SECOND; throw new Error('synthetic lost password ACK') })
  await expect(nativeApiTransport('/api/auth/password', password)).rejects.toThrow('lost password ACK')
  expect(() => captureNativeTrustPresentation()).toThrow('unavailable')
  expect(invoke.mock.calls.map(c => c[0])).toEqual(['native_auth_password'])
  await nativeApiTransport('/api/auth/login', login)
  expect(invoke.mock.calls.map(c => c[0])).toEqual(['native_auth_password', 'native_context', 'native_auth_login'])
  expect(invoke.mock.calls[2]?.[1]?.authenticationIntent).toBe(SECOND)
})
it('never sends pending explicit-login credentials after profile changes during descriptor recovery', async () => {
  invoke.mockImplementationOnce(async () => { throw new Error('synthetic lost login ACK') })
  await expect(nativeApiTransport('/api/auth/login', login)).rejects.toThrow()
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const old = nativeApiTransport('/api/auth/login', login)
  await connectNative('other.example.invalid')
  finish({ context: CONTEXT, profile_intent: PROFILE, authentication_intent: SECOND, content_authorization: 'unavailable', remembered_login: false })
  await expect(old).rejects.toMatchObject({ name: 'AbortError' })
  expect(invoke.mock.calls.filter(c => c[0] === 'native_auth_login')).toHaveLength(1)
})
