import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { userFixture } from '../test-fixtures.fixture'

const lib = vi.hoisted(() => ({
  listener: null as null | ((request: { channel_id: string, message_id: string, parent_id: string | null }) => void),
  stop: vi.fn(),
  refresh: vi.fn(async () => true),
}))

vi.mock('../lib/webPush', () => ({
  listenForOpenRequests: (open: typeof lib.listener) => { lib.listener = open; return lib.stop },
  refreshPushState: lib.refresh,
}))

import { usePushBridge } from './usePushBridge'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { currentRoute } from '../lib/router'

const Host = defineComponent({ setup() { usePushBridge(); return () => null } })

beforeEach(() => {
  setActivePinia(createPinia())
  lib.listener = null
  lib.stop.mockClear()
  lib.refresh.mockClear()
})
afterEach(() => vi.unstubAllGlobals())

describe('the push bridge', () => {
  it('opens the message a notification click names, a thread reply at its root', async () => {
    const chat = useChatStore()
    const goTo = vi.spyOn(chat, 'goToMessage').mockImplementation(() => {})
    const w = mount(Host)
    lib.listener?.({ channel_id: 'ch1', message_id: 'm1', parent_id: null })
    lib.listener?.({ channel_id: 'ch1', message_id: 'm2', parent_id: 'm0' })
    expect(goTo.mock.calls.map(c => c[0])).toEqual([
      { id: 'm1', channel_id: 'ch1' },
      { id: 'm2', channel_id: 'ch1', parent_id: 'm0' },
    ])
    w.unmount()
    expect(lib.stop).toHaveBeenCalled()
  })

  it('navigates for real through the chat store', () => {
    const w = mount(Host)
    lib.listener?.({ channel_id: 'ch9', message_id: 'mx', parent_id: null })
    expect(currentRoute.value).toMatchObject({ view: 'chat', channelRef: 'ch9', messageId: 'mx' })
    w.unmount()
  })

  it('asks whether this device receives push once the account is known, not before', async () => {
    const w = mount(Host)
    expect(lib.refresh).not.toHaveBeenCalled()
    useAuthStore().user = userFixture({ id: 'me' })
    await nextTick()
    expect(lib.refresh).toHaveBeenCalledTimes(1)
    w.unmount()
  })
})
