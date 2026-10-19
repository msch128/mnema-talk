import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { fixtureId, userFixture } from '../test-fixtures.fixture'

const made = vi.hoisted(() => ({ sockets: [] as { path: string; closed: boolean }[] }))
vi.mock('../lib/native/trustedSocket', () => ({
  TrustedSocket: class {
    onopen: (() => void) | null = null
    onclose: (() => void) | null = null
    onmessage: ((event: { data: string }) => void) | null = null
    readyState = 0
    closed = false
    path: string
    constructor(path = '/api/ws') { this.path = path; made.sockets.push(this) }
    send() {}
    close() { this.closed = true }
  },
}))
import { useChatStore } from './chat'
import { useAuthStore } from './auth'

beforeEach(() => {
  setActivePinia(createPinia())
  made.sockets.length = 0
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it('talks to the server through the desktop host in the trusted desktop client, never through a browser WebSocket', () => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL('mnema://localhost/'))
  vi.stubGlobal('__MNEMA_TRUSTED_APP__', { version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'instance' })
  vi.stubGlobal('WebSocket', vi.fn(() => { throw new Error('a browser WebSocket would reach the wrong origin') }))
  useAuthStore().user = userFixture({ id: fixtureId(1) })
  const chat = useChatStore()
  chat.initWebSocket()
  expect(made.sockets).toHaveLength(1)
  expect(made.sockets[0]?.path).toBe('/api/ws')
  chat.closeWebSocket()
  expect(made.sockets[0]?.closed).toBe(true)
})
