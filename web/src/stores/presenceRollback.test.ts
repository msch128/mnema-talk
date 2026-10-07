import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { useChatStore } from './chat'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => { useChatStore().closeWebSocket(); vi.unstubAllGlobals() })

function delayedFailure() {
  let fail: ((reason: Error) => void) | undefined
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(() => new Promise<Response>((_resolve, reject) => { fail = reject }))
  vi.stubGlobal('fetch', fetch)
  return { fetch, fail: () => required(fail)(new TypeError('synthetic offline failure')) }
}

describe('failed presence update rollback', () => {
  it('restores only presence and preserves account updates that arrived during the request', async () => {
    const chat = useChatStore()
    const auth = useAuthStore()
    auth.user = userFixture({ display_name: 'Before', presence: 'online' })
    const request = delayedFailure()
    const change = chat.setMyPresence('dnd')
    const rejected = expect(change).rejects.toThrow()
    expect(auth.user.presence).toBe('dnd')
    auth.user = { ...auth.user, display_name: 'After', bio: 'Concurrent profile update', status_text: 'New status' }
    request.fail()
    await rejected
    expect(auth.user).toEqual(userFixture({ display_name: 'After', bio: 'Concurrent profile update', status_text: 'New status', presence: 'online' }))
    expect(request.fetch).toHaveBeenCalledWith('/api/users/me/presence', expect.objectContaining({ method: 'PUT', body: JSON.stringify({ presence: 'dnd' }) }))
  })

  it('does not resurrect an account that logged out while its request was pending', async () => {
    const chat = useChatStore()
    const auth = useAuthStore()
    auth.user = userFixture({ presence: 'online' })
    const request = delayedFailure()
    const rejected = expect(chat.setMyPresence('away')).rejects.toThrow()
    auth.user = null
    request.fail()
    await rejected
    expect(auth.user).toBeNull()
  })

  it('does not replace the newly signed-in account after an account switch', async () => {
    const chat = useChatStore()
    const auth = useAuthStore()
    auth.user = userFixture({ presence: 'online' })
    const request = delayedFailure()
    const rejected = expect(chat.setMyPresence('focus')).rejects.toThrow()
    const replacement = userFixture({ id: fixtureId(9), username: 'other-member', presence: 'away' })
    auth.user = replacement
    request.fail()
    await rejected
    expect(auth.user).toEqual(replacement)
  })

  it('removes the optimistic presence when the account had no stored presence', async () => {
    const chat = useChatStore()
    const auth = useAuthStore()
    auth.user = userFixture()
    const request = delayedFailure()
    const rejected = expect(chat.setMyPresence('away')).rejects.toThrow()
    auth.user = { ...auth.user, display_name: 'Concurrent name' }
    request.fail()
    await rejected
    expect(auth.user?.display_name).toBe('Concurrent name')
    expect(auth.user).not.toHaveProperty('presence')
  })
})
