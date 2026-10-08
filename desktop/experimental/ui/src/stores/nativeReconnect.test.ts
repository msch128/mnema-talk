// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { installNativePort, initializeNativeContext } from '../lib/nativeTransport'
import type { NativeChannel, NativeNotice, NativePort } from '../lib/nativeTransport'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { memoryStorageFixture, userFixture } from '../test-fixtures.fixture'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const HANDLE = '00000000-0000-4000-8000-000000000002'
let channel: NativeChannel
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
let pinia: ReturnType<typeof createPinia>
const reply = (status: number, body: unknown = null) => ({ context: CONTEXT, status, body })
async function flush() { for (let i = 0; i < 12; i++) await Promise.resolve() }
function notice(kind: NativeNotice['kind'], sequence: number) {
  channel.onmessage({ context: CONTEXT, handle: HANDLE, sequence, kind, payload: kind === 'closed' ? { code: 'NATIVE_SOCKET_CLOSED' } : null })
}
beforeEach(async () => {
  pinia = createPinia(); setActivePinia(pinia)
  vi.stubGlobal('localStorage', memoryStorageFixture()); vi.useFakeTimers()
  channel = { onmessage: () => {} }
  invoke = vi.fn(async command => {
    if (command === 'native_context') return { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: CONTEXT }
    if (command === 'native_socket_open') return reply(200, { handle: HANDLE })
    if (command === 'native_auth_me') return reply(200, userFixture({ locale: 'de' }))
    return reply(204)
  })
  installNativePort({ invoke, channel: () => channel }); await initializeNativeContext()
  useAuthStore().user = userFixture({ locale: 'de' })
})
afterEach(() => {
  useChatStore().closeWebSocket(); disposePinia(pinia)
  vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals()
})
describe('actual native reconnect with shared auth store', () => {
  it('revalidates with native Me after generic closure and clears only its real401', async () => {
    const chat = useChatStore(); chat.initWebSocket(); await flush()
    notice('opened', 1); await flush()
    invoke.mockImplementation(async command => {
      if (command === 'native_auth_me') return reply(401, { error: { code: 'UNAUTHORIZED', message: 'fixture expired' } })
      return reply(204)
    })
    notice('closed', 2)
    expect(useAuthStore().isAuthenticated).toBe(true)
    await vi.advanceTimersByTimeAsync(1000); await flush()
    expect(invoke).toHaveBeenCalledWith('native_auth_me', expect.objectContaining({ context: CONTEXT }))
    expect(useAuthStore().isAuthenticated).toBe(false)
    expect(invoke.mock.calls.filter(([command]) => command === 'native_socket_open')).toHaveLength(1)
  })
  it('retains account and retries when native Me fails through a network outage', async () => {
    const chat = useChatStore(); chat.initWebSocket(); await flush()
    notice('opened', 1); await flush()
    invoke.mockImplementation(async command => {
      if (command === 'native_auth_me') throw new Error('synthetic network failure')
      return reply(204)
    })
    notice('closed', 2); await vi.advanceTimersByTimeAsync(1000); await flush()
    expect(useAuthStore().isAuthenticated).toBe(true)
    expect(chat.reconnectAttempt).toBe(2)
    expect(invoke.mock.calls.filter(([command]) => command === 'native_socket_open')).toHaveLength(1)
  })
  it('ignores an old native Me401 after a same-account login rotates the session', async () => {
    let finish!: (value: unknown) => void
    const stale = new Promise<unknown>(resolve => { finish = resolve })
    invoke.mockImplementation(async command => {
      if (command === 'native_auth_me') return stale
      if (command === 'native_auth_login') return reply(200, { user: userFixture({ locale: 'de' }) })
      return reply(204)
    })
    const auth = useAuthStore()
    const pending = auth.checkAuth(); await flush()
    await auth.login('member', 'synthetic-only-password')
    finish(reply(401, { error: { code: 'UNAUTHORIZED', message: 'old fixture session' } }))
    expect(await pending).toBeNull()
    expect(auth.isAuthenticated).toBe(true)
  })
  it('does not revalidate or reconnect after explicit owned teardown', async () => {
    const chat = useChatStore(); chat.initWebSocket(); await flush()
    invoke.mockClear(); chat.closeWebSocket()
    await vi.advanceTimersByTimeAsync(31000); await flush()
    expect(invoke.mock.calls.some(([command]) => command === 'native_auth_me' || command === 'native_socket_open')).toBe(false)
    expect(useAuthStore().isAuthenticated).toBe(true)
  })
})
