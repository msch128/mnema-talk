import { beforeEach, describe, expect, it, vi } from 'vitest'
import { connectNative, disconnectNative, initializeNativeContext, installNativePort, nativeApiTransport, NativeSocket } from './nativeTransport'
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
    if (command === 'native_context') return { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, authentication_intent: '50000000-0000-4000-8000-000000000001', profile_intent: CONTEXT }
    if (command === 'native_connect') return reply(200, { origin: 'https://example.invalid', community_id: 'synthetic', compatibility: 'supported', transport_preview: true, content_authorization: 'unavailable', profile_intent: OTHER })
    if (command === 'native_socket_open') return reply(200, { handle: HANDLE })
    return reply(204)
  })
  installNativePort({ invoke, channel: () => channel })
  await initializeNativeContext()
})
describe('native API delivery', () => {
  it('rejects aborted metadata before IPC and tolerates failure of the captured cancellation request', async () => {
    invoke.mockClear()
    await expect(nativeApiTransport('/api/auth/me', { method: 'GET', signal: AbortSignal.abort() })).rejects.toMatchObject({ name: 'AbortError' })
    expect(invoke).not.toHaveBeenCalled()
    const d = deferred<unknown>(), abort = new AbortController()
    invoke.mockImplementationOnce(() => d.promise)
    const pending = nativeApiTransport('/api/auth/me', { method: 'GET', signal: abort.signal })
    invoke.mockRejectedValueOnce(new Error('Cancellation unavailable')); abort.abort(); d.resolve(reply(200, {}))
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' }); await tick()
  })
  it('bounds discovery addresses and rejects corrupted recovery descriptors before any connection', async () => {
    invoke.mockClear(); await expect(connectNative('x'.repeat(2049))).rejects.toThrow()
    expect(invoke).not.toHaveBeenCalled()
    await disconnectNative()
    const valid = { context: CONTEXT, profile_intent: null, authentication_intent: null, remembered_login: false, content_authorization: 'unavailable' }
    for (const value of [{ ...valid, context: OTHER }, { ...valid, profile_intent: 'forged' }, { ...valid, authentication_intent: 'forged' }]) {
      invoke.mockResolvedValueOnce(value); await expect(connectNative('community.example.invalid')).rejects.toThrow()
    }
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const pending = connectNative('community.example.invalid'); installNativePort({ invoke, channel: () => channel })
    d.resolve(valid); await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    invoke.mockClear(); await disconnectNative(); expect(invoke).not.toHaveBeenCalled()
  })
  it('surfaces failed native disconnect while sealing the previous local selector', async () => {
    invoke.mockResolvedValueOnce(reply(503)); await expect(disconnectNative()).rejects.toThrow('disconnect unavailable')
    await expect(nativeApiTransport('/api/auth/me', { method: 'GET' })).rejects.toThrow('profile unavailable')
  })
  it('rejects malformed credential inputs without dispatch and handles rejected authentication ACKs', async () => {
    invoke.mockClear()
    for (const [path, json] of [
      ['/api/auth/login', { username: 1, password: 'synthetic' }],
      ['/api/auth/password', { current_password: false, new_password: 'synthetic' }],
      ['/api/auth/register', { username: 'synthetic', display_name: '', password: null, invite_code: 'synthetic' }]
    ] as const) expect(await nativeApiTransport(path, { method: path.includes('password') ? 'PUT' : 'POST', json })).toMatchObject({ status: 503 })
    expect(invoke).not.toHaveBeenCalled()
    invoke.mockResolvedValueOnce(reply(403, { error: 'Denied' }))
    expect(await nativeApiTransport('/api/auth/password', { method: 'PUT', json: { current_password: 'synthetic-old', new_password: 'synthetic-new' } })).toEqual({ status: 403, body: { error: 'Denied' } })
    invoke.mockResolvedValueOnce(reply(401))
    expect(await nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'synthetic', password: 'synthetic' } })).toMatchObject({ status: 401 })
  })
  it('rejects closed-context violations and drops a descriptor delivered after port replacement', async () => {
    const valid = { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, authentication_intent: HANDLE, profile_intent: CONTEXT }
    for (const value of [null, { ...valid, context: '00000000-0000-0000-0000-000000000000' }, { ...valid, remembered_login: true },
      { ...valid, authentication_intent: 'forged' }, { ...valid, profile_intent: null }]) {
      invoke.mockResolvedValueOnce(value); await expect(initializeNativeContext()).rejects.toThrow()
    }
    const pending = deferred<unknown>(); invoke.mockImplementationOnce(() => pending.promise)
    const old = initializeNativeContext(); installNativePort({ invoke, channel: () => channel })
    pending.resolve(valid); await expect(old).rejects.toMatchObject({ name: 'AbortError' })
    const before = invoke.mock.calls.length
    await expect(nativeApiTransport('/api/auth/me', { method: 'GET' })).rejects.toThrow('context unavailable')
    await expect(nativeApiTransport('/api/auth/me', { method: 'GET', signal: AbortSignal.abort() })).rejects.toThrow()
    expect(invoke.mock.calls).toHaveLength(before)
  })
  it('maps only exact auth methods and keeps password bytes intact', async () => {
    invoke.mockResolvedValueOnce(reply(200, { user: { username: 'fixture' }, authentication_intent: '50000000-0000-4000-8000-000000000002' }))
    expect(await nativeApiTransport('/api/auth/login', { method: 'POST', json: { username: 'fixture', password: '  fixture\npassword  ' } })).toEqual({ status: 200, body: { user: { username: 'fixture' }, authentication_intent: '50000000-0000-4000-8000-000000000002' } })
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
  it('suppresses errors cleaning up a returned handle belonging to an already retired socket', async () => {
    const d = deferred<unknown>(); invoke.mockImplementationOnce(() => d.promise)
    const socket = new NativeSocket(); socket.close()
    invoke.mockRejectedValueOnce(new Error('Late cleanup unavailable')); d.resolve(reply(200, { handle: HANDLE }))
    await tick(); await tick(); expect(socket.readyState).toBe(3)
  })
  it('accepts bounded idle actions and rejects malformed or oversized renderer actions', async () => {
    const socket = new NativeSocket()
    expect(() => socket.send('{}')).toThrow('unavailable')
    await tick(); notice(1, 'opened')
    for (const data of ['x'.repeat(4097), 'invalid-json', 'null', JSON.stringify({ type: 'ping', payload: null }), JSON.stringify({ type: 'ping', payload: { t: -1 } }), JSON.stringify({ type: 'presence_idle', payload: { idle: 'true' } })]) expect(() => socket.send(data)).toThrow()
    socket.send(JSON.stringify({ type: 'presence_idle', payload: { idle: true } }))
    expect(invoke).toHaveBeenLastCalledWith('native_socket_send', expect.objectContaining({ action: { type: 'presence_idle', payload: { idle: true } } }))
    socket.close()
  })
  it.each(['send-refused', 'send-failed', 'open-failed', 'close-failed', 'cancel-failed'])('retires native socket after %s', async mode => {
    if (mode === 'open-failed') invoke.mockRejectedValueOnce(new Error('Open unavailable'))
    if (mode === 'cancel-failed') invoke.mockImplementationOnce(() => new Promise(() => {}))
    const socket = new NativeSocket(); await tick()
    if (mode === 'open-failed') { expect(socket.readyState).toBe(3); return }
    if (mode === 'cancel-failed' || mode === 'close-failed') {
      invoke.mockRejectedValueOnce(new Error('Retirement unavailable')); socket.close()
    } else {
      notice(1, 'opened')
      if (mode === 'send-refused') invoke.mockResolvedValueOnce(reply(503)); else invoke.mockRejectedValueOnce(new Error('Send unavailable'))
      socket.send(JSON.stringify({ type: 'ping', payload: { t: 1 } }))
    }
    await tick(); await tick(); expect(socket.readyState).toBe(3)
  })
  it.each(['foreign-handle', 'malformed', 'oversized', 'closed', 'before-open'])('handles %s native notices without unauthorized delivery', async mode => {
    const socket = new NativeSocket(); await tick(); const message = vi.fn(); socket.onmessage = message
    if (mode !== 'before-open') notice(1, 'opened')
    if (mode === 'foreign-handle') notice(2, 'message', { type: 'pong' }, OTHER)
    if (mode === 'malformed') channel.onmessage({ context: CONTEXT, handle: HANDLE, sequence: 0, kind: 'message', payload: null })
    if (mode === 'oversized') notice(2, 'message', { type: 'pong', payload: 'x'.repeat(65536) })
    if (mode === 'closed') notice(2, 'closed')
    if (mode === 'before-open') notice(1, 'message', { type: 'pong' })
    expect(message).not.toHaveBeenCalled(); expect(socket.readyState).toBe(mode === 'foreign-handle' ? socket.OPEN : 3)
    socket.close()
  })
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
    expect(invoke).toHaveBeenCalledWith('native_socket_close', { context: CONTEXT, handle: HANDLE, profileIntent: CONTEXT, authenticationIntent: '50000000-0000-4000-8000-000000000001' })
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
