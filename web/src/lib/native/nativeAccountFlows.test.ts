import { beforeEach, expect, it, vi } from 'vitest'
import { connectNative, initializeNativeContext, installNativePort, nativeApiTransport } from './nativeTransport'
const CONTEXT = '10000000-0000-4000-8000-000000000001'
const FIRST = '20000000-0000-4000-8000-000000000001'
const SECOND = '20000000-0000-4000-8000-000000000002'
const ALICE = '30000000-0000-4000-8000-000000000001'
const BOB = '30000000-0000-4000-8000-000000000002'
const registration = { method: 'POST', json: { username: 'synthetic-alice', display_name: 'Synthetic Alice', password: 'synthetic-fixture-password', invite_code: 'synthetic-fixture-invite' } }
let selected: string | null
const reply = (status: number, body: unknown) => ({ context: CONTEXT, status, body })
const invoke = vi.fn(async (command: string, _args?: Record<string, unknown>): Promise<unknown> => {
  if (command === 'native_context') return { context: CONTEXT, profile_intent: selected, content_authorization: 'unavailable', remembered_login: false, authentication_intent: selected ? '50000000-0000-4000-8000-000000000001' : null }
  if (command === 'native_connect') { selected = SECOND; return reply(200, { profile_intent: SECOND }) }
  if (command === 'native_auth_register') return reply(201, { user: { id: ALICE } })
  if (command === 'native_auth_login') return reply(200, { user: { id: ALICE }, authentication_intent: '50000000-0000-4000-8000-000000000002' })
  if (command === 'native_auth_password') return reply(200, { authentication_intent: '50000000-0000-4000-8000-000000000002' })
  return reply(204, null)
})
beforeEach(async () => { selected = FIRST; invoke.mockClear(); installNativePort({ invoke, channel: () => ({ onmessage: () => {} }) }); await initializeNativeContext(); invoke.mockClear() })
it('requires a distinct matching native login after acknowledged invite registration', async () => {
  expect(await nativeApiTransport('/api/auth/register', registration)).toEqual({ status: 200, body: { user: { id: ALICE }, authentication_intent: '50000000-0000-4000-8000-000000000002' } })
  expect(invoke.mock.calls.map(c => c[0])).toEqual(['native_auth_register', 'native_auth_login'])
  expect(invoke.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ profileIntent: FIRST, username: 'synthetic-alice', displayName: 'Synthetic Alice', inviteCode: 'synthetic-fixture-invite' }))
  expect(invoke.mock.calls[1]?.[1]).toEqual(expect.objectContaining({ profileIntent: FIRST, password: 'synthetic-fixture-password' }))
})
it('does not retry uncertain account creation or login without acknowledgement', async () => {
  invoke.mockImplementationOnce(async () => { throw new Error('synthetic lost acknowledgement') })
  expect(await nativeApiTransport('/api/auth/register', registration)).toMatchObject({ status: 503, body: { error: { code: 'LOGIN_REQUIRED' } } })
  expect(invoke.mock.calls.map(c => c[0])).toEqual(['native_auth_register'])
})
it('preserves rejected invites and rejects malformed creation without login', async () => {
  invoke.mockImplementationOnce(async () => reply(409, { error: { code: 'INVITE_USED' } }))
  expect(await nativeApiTransport('/api/auth/register', registration)).toMatchObject({ status: 409 })
  invoke.mockImplementationOnce(async () => reply(201, { user: { id: 'invalid' } }))
  expect(await nativeApiTransport('/api/auth/register', registration)).toMatchObject({ status: 503 })
  expect(invoke.mock.calls.some(c => c[0] === 'native_auth_login')).toBe(false)
})
it('seals the captured profile if login identifies a different account', async () => {
  invoke.mockImplementationOnce(async () => reply(201, { user: { id: ALICE } }))
  invoke.mockImplementationOnce(async () => reply(200, { user: { id: BOB }, authentication_intent: '50000000-0000-4000-8000-000000000002' }))
  expect(await nativeApiTransport('/api/auth/register', registration)).toMatchObject({ status: 503 })
  expect(invoke.mock.calls.map(c => c[0])).toEqual(['native_auth_register', 'native_auth_login', 'native_auth_logout'])
  expect(invoke.mock.calls[2]?.[1]).toEqual(expect.objectContaining({ context: CONTEXT, profileIntent: FIRST }))
})
it('never sends an old registration password to a replacement profile after delayed creation', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const pending = nativeApiTransport('/api/auth/register', registration)
  await connectNative('second.example.invalid')
  finish(reply(201, { user: { id: ALICE } }))
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  expect(invoke.mock.calls.some(c => c[0] === 'native_auth_login')).toBe(false)
  expect(invoke.mock.calls[0]?.[1]?.profileIntent).toBe(FIRST)
})
it('uses fixed password arguments and accepts no replacement grant in its acknowledgement', async () => {
  const request = { method: 'PUT', json: { current_password: 'synthetic-old', new_password: 'synthetic-new' } }
  expect(await nativeApiTransport('/api/auth/password', request)).toEqual({ status: 204, body: null })
  expect(invoke.mock.calls[0]).toEqual(['native_auth_password', expect.objectContaining({ profileIntent: FIRST, currentPassword: 'synthetic-old', newPassword: 'synthetic-new' })])
  invoke.mockImplementationOnce(async () => reply(200, { authentication_intent: '50000000-0000-4000-8000-000000000002', unexpected: true }))
  await expect(nativeApiTransport('/api/auth/password', request)).rejects.toThrow('acknowledgement')
})
it('denies extra credential fields and arbitrary admin endpoints before native dispatch', async () => {
  expect(await nativeApiTransport('/api/auth/register', { ...registration, json: { ...registration.json, admin: true } })).toMatchObject({ status: 503 })
  expect(await nativeApiTransport('/api/auth/password', { method: 'PUT', json: { current_password: 'synthetic-old', new_password: 'synthetic-new', profileIntent: SECOND } })).toMatchObject({ status: 503 })
  expect(await nativeApiTransport('/api/admin/system/run', { method: 'POST', json: { command: 'anything' } })).toMatchObject({ status: 503 })
  expect(invoke).not.toHaveBeenCalled()
  await nativeApiTransport(`/api/admin/users/${BOB}/disable`, { method: 'POST' })
  expect(invoke.mock.calls[0]).toEqual(['native_admin_request', expect.objectContaining({ profileIntent: FIRST, input: { operation: 'disable_user', id: BOB } })])
})
it('blocks registration and password dispatch without an acknowledged selected profile', async () => {
  selected = null; await initializeNativeContext(); invoke.mockClear()
  await expect(nativeApiTransport('/api/auth/register', registration)).rejects.toThrow('profile unavailable')
  await expect(nativeApiTransport('/api/auth/password', { method: 'PUT', json: { current_password: 'synthetic-old', new_password: 'synthetic-new' } })).rejects.toThrow('profile unavailable')
  expect(invoke).not.toHaveBeenCalled()
})
