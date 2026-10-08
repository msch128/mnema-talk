import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectNative, initializeNativeContext, installNativePort, nativeApiTransport, NativeSocket } from './nativeTransport'
import type { NativeChannel, NativeNotice, NativePort } from './nativeTransport'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const HANDLE = '00000000-0000-4000-8000-000000000002'
const OTHER = '00000000-0000-4000-8000-000000000003'
let channel: NativeChannel
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
function reply(status: number, body: unknown = null) { return { context: CONTEXT, status, body } }
const tick = async () => { await Promise.resolve(); await Promise.resolve() }
function notice(sequence: number, kind: NativeNotice['kind'], payload: unknown = null, handle = HANDLE) { channel.onmessage({ context: CONTEXT, handle, sequence, kind, payload }) }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
beforeEach(async () => {
  channel = { onmessage: () => {} }
  invoke = vi.fn(async (command: string) => {
    if (command === 'native_context') return { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: CONTEXT }
    if (command === 'native_connect') return reply(200, { origin: 'https://example.invalid', community_id: 'synthetic', compatibility: 'supported', transport_preview: true, content_authorization: 'unavailable', profile_intent: OTHER })
    if (command === 'native_socket_open') return reply(200, { handle: HANDLE })
    return reply(204)
  })
  installNativePort({ invoke, channel: () => channel })
  await initializeNativeContext()
})
describe('native API delivery', () => {
  it('maps only exact auth methods and keeps password bytes intact', async () => {
    invoke.mockResolvedValueOnce(reply(200, { user: { username: 'fixture' } }))
    expect(await nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'fixture', password: '  fixture\npassword  ' } })).toEqual({ status: 200, body: { user: { username: 'fixture' } } })
    expect(invoke).toHaveBeenLastCalledWith('native_auth_login', expect.objectContaining({ context: CONTEXT, username: 'fixture', password: '  fixture\npassword  ', requestId: expect.any(String) }))
    invoke.mockClear()
    for (const [path, method] of [['/api/messages', 'GET'], ['/api/auth/login', 'GET'], ['https://foreign.invalid/api/auth/me', 'GET']] as const) expect((await nativeApiTransport(path, { method })).status).toBe(503)
    expect(invoke).not.toHaveBeenCalled()
  })
  it('uses fixed public metadata commands before login and authenticated commands for community data', async () => {
    invoke.mockClear()
    for (const [path, resource] of [['/api/legal', 'legal'], ['/api/health', 'health']] as const) {
      await nativeApiTransport(path, { method: 'GET' })
      expect(invoke).toHaveBeenLastCalledWith('native_public_metadata_request', expect.objectContaining({ context: CONTEXT, resource }))
    }
    await nativeApiTransport('/api/members', { method: 'GET' })
    expect(invoke).toHaveBeenLastCalledWith('native_metadata_request', expect.objectContaining({ resource: 'members' }))
    const calls = invoke.mock.calls.length
    expect((await nativeApiTransport('/api/legal?origin=other', { method: 'GET' })).status).toBe(503)
    expect((await nativeApiTransport('/api/legal', { method: 'POST' })).status).toBe(503)
    expect(invoke.mock.calls).toHaveLength(calls)
  })
  it('maps five personal actions to fixed closed inputs including Unicode profile text', async () => {
    for (const [path, method, json, input] of [
      [`/api/users/${OTHER}`, 'GET', undefined, { operation: 'user', user_id: OTHER }],
      ['/api/users/me/profile', 'PUT', { display_name: '🌲'.repeat(24), bio: '新しい自己紹介' }, { operation: 'profile', display_name: '🌲'.repeat(24), bio: '新しい自己紹介' }],
      ['/api/users/me/locale', 'PUT', { locale: 'de' }, { operation: 'locale', locale: 'de' }],
      ['/api/users/me/presence', 'PUT', { presence: 'focus' }, { operation: 'presence', presence: 'focus' }],
      ['/api/users/me/status', 'PUT', { status_text: '' }, { operation: 'status', status_text: '' }],
    ] as const) {
      invoke.mockResolvedValueOnce(reply(200, { id: OTHER }))
      const request = json === undefined ? { method } : { method, json }
      expect((await nativeApiTransport(path, request)).status).toBe(200)
      expect(invoke).toHaveBeenLastCalledWith('native_personal_metadata_request', expect.objectContaining({ context: CONTEXT, input, requestId: expect.any(String) }))
    }
    invoke.mockClear()
    for (const [path, method, json] of [
      ['/api/users/me/profile', 'PUT', { display_name: '🌲'.repeat(25), bio: '' }],
      ['/api/users/me/profile', 'PUT', { display_name: 'name', bio: '', role: 'admin' }],
      ['/api/users/me/locale', 'PUT', { locale: 'fr' }],
      ['/api/users/me/presence', 'PUT', { presence: 'offline' }],
      ['/api/users/me/status', 'PUT', { status_text: 'x'.repeat(33) }],
      [`/api/users/${OTHER}?token=synthetic`, 'GET', undefined],
      [`/api/users/${OTHER}`, 'DELETE', undefined],
    ] as const) expect((await nativeApiTransport(path, json === undefined ? { method } : { method, json })).status).toBe(503)
    expect(invoke).not.toHaveBeenCalled()
  })
  it('cancels a pending profile result after switching the server context', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const pending = nativeApiTransport('/api/users/me/status', { method: 'PUT', json: { status_text: 'own fixture' } })
    await connectNative('other.example.invalid'); d.resolve(reply(200, { id: OTHER, status_text: 'own fixture' }))
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  })
  it('cancels an outstanding native label and rejects a late successful reply', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const abort = new AbortController(); const pending = nativeApiTransport('/api/auth/me', { method: 'GET', signal: abort.signal })
    abort.abort(); d.resolve(reply(200, { id: 'fixture' }))
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    expect(invoke).toHaveBeenCalledWith('native_request_cancel', expect.objectContaining({ context: CONTEXT, requestId: expect.any(String) }))
  })
  it('turns old account 401 after a profile switch into cancellation', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const old = nativeApiTransport('/api/auth/me', { method: 'GET' }); await connectNative('community.example.invalid')
    d.resolve(reply(401, { error: { code: 'UNAUTHORIZED' } })); await expect(old).rejects.toMatchObject({ name: 'AbortError' })
  })
  it('rejects mismatched context and maps native stale status to cancellation', async () => {
    invoke.mockResolvedValueOnce({ context: OTHER, status: 200, body: {} }); await expect(nativeApiTransport('/api/auth/me', { method: 'GET' })).rejects.toThrow('Invalid native reply')
    invoke.mockResolvedValueOnce(reply(499)); await expect(nativeApiTransport('/api/auth/me', { method: 'GET' })).rejects.toMatchObject({ name: 'AbortError' })
  })
})
describe('native socket ownership', () => {
  it('buffers early notices until native handle validation and rejects replay', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const socket = new NativeSocket(); const opened = vi.fn(); socket.onopen = opened
    notice(1, 'opened'); expect(opened).not.toHaveBeenCalled(); d.resolve(reply(200, { handle: HANDLE })); await tick()
    expect(opened).toHaveBeenCalledOnce(); expect(socket.readyState).toBe(socket.OPEN)
    const message = vi.fn(); socket.onmessage = message; notice(3, 'message', { type: 'pong', payload: { t: 1 } })
    expect(message).toHaveBeenCalledWith({ data: JSON.stringify({ type: 'pong', payload: { t: 1 } }) })
    notice(3, 'message', { type: 'pong', payload: {} }); expect(socket.readyState).toBe(3)
  })
  it('delivers bounded native metadata updates needed for disabled members and channel refresh', async () => {
    const socket = new NativeSocket(); await tick(); notice(1, 'opened')
    const message = vi.fn(); socket.onmessage = message
    const events = [
      { type: 'channels_changed', payload: null },
      { type: 'member_joined', payload: { id: OTHER, username: 'fixture' } },
      { type: 'user_update', payload: { id: OTHER, disabled: true } },
      { type: 'user_stats', payload: { user_id: OTHER, voice_seconds: 5 } },
    ]
    events.forEach((event, index) => notice(index + 2, 'message', event))
    expect(message.mock.calls.map(([event]) => JSON.parse(event.data))).toEqual(events)
    expect(socket.readyState).toBe(socket.OPEN)
  })
  it('closes a late returned handle after teardown and ignores notices', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const socket = new NativeSocket(); const opened = vi.fn(); socket.onopen = opened; socket.close()
    d.resolve(reply(200, { handle: HANDLE })); await tick(); notice(1, 'opened'); expect(opened).not.toHaveBeenCalled()
    expect(invoke).toHaveBeenCalledWith('native_socket_close', { context: CONTEXT, handle: HANDLE, profileIntent: CONTEXT })
  })
  it('blocks plaintext-content and voice events in both directions', async () => {
    const socket = new NativeSocket(); await tick(); notice(1, 'opened'); invoke.mockClear()
    expect(() => socket.send(JSON.stringify({ type: 'voice_join', payload: { channel_id: OTHER } }))).toThrow('noch nicht verfügbar'); expect(invoke).not.toHaveBeenCalled()
    socket.send(JSON.stringify({ type: 'ping', payload: { t: 5 } })); expect(invoke).toHaveBeenCalledWith('native_socket_send', expect.objectContaining({ handle: HANDLE, action: { type: 'ping', payload: { t: 5 } } }))
    const message = vi.fn(); socket.onmessage = message; notice(2, 'message', { type: 'message_created', payload: { content: 'synthetic-private-content' } })
    expect(message).not.toHaveBeenCalled(); expect(socket.readyState).toBe(3)
  })
  it('bounds pre-ack callbacks and cannot open after profile replacement', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const socket = new NativeSocket(); const opened = vi.fn(); socket.onopen = opened
    for (let i = 1; i <= 33; i++) notice(i, 'opened'); expect(socket.readyState).toBe(3)
    await connectNative('another.example.invalid'); d.resolve(reply(200, { handle: HANDLE })); await tick(); expect(opened).not.toHaveBeenCalled()
  })
})
