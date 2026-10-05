// Route handling in App.vue: a route that is still loading when the next one
// arrives must not act on the newer state.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './stores/auth'
import { useChatStore } from './stores/chat'
import { navigate } from './lib/router'
import App from './App.vue'

vi.mock('./composables/useWebRTC', () => ({
  useWebRTC: () => ({
    resumeVoiceSession: vi.fn(async () => false),
    resumeRemoteAudio: vi.fn(),
    leaveVoiceChannel: vi.fn(),
    rejoinAfterReconnect: vi.fn()
  })
}))

const channels = [
  { id: 'ch1', name: 'eins', type: 'text' },
  { id: 'ch2', name: 'zwei', type: 'text' },
  { id: 'ch3', name: 'drei', type: 'text' }
]

let wrapper
beforeEach(() => {
  window.history.replaceState(null, '', '/')
  navigate('/', { replace: true })
  setActivePinia(createPinia())
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

async function mountApp() {
  const auth = useAuthStore()
  const chat = useChatStore()
  auth.checkAuth = vi.fn(async () => {
    auth.user = { id: 'me', username: 'max', role: 'user' }
    return true
  })
  chat.fetchChannels = vi.fn(async () => { chat.categories = [{ id: 'c', channels }] })
  chat.fetchMembers = vi.fn(async () => {})
  chat.fetchReadState = vi.fn(async () => {})
  chat.initWebSocket = vi.fn()
  // Channel switches wait until the test lets them finish.
  const selects = []
  chat.selectChannel = vi.fn(channel => new Promise(resolve => {
    selects.push(() => { chat.activeChannel = channel; resolve() })
  }))
  chat.openThread = vi.fn(async id => { chat.activeThread = { id, user_id: 'u', content: 'x' } })
  wrapper = mount(App, { shallow: true })
  await flushPromises()
  return { chat, selects }
}

describe('App routing', () => {
  it('a superseded route does not open its thread in the newer channel', async () => {
    const { chat, selects } = await mountApp()
    // The initial "/" route picked the first channel.
    selects.splice(0).forEach(done => done())
    await flushPromises()

    navigate('/c/ch2/t/t1')
    await flushPromises()
    navigate('/c/ch3')
    await flushPromises()
    expect(selects).toHaveLength(2)
    // The newer route lands first, then the older one's switch completes.
    selects[1]()
    await flushPromises()
    selects[0]()
    await flushPromises()

    expect(chat.openThread).not.toHaveBeenCalled()
    expect(chat.activeThread).toBeNull()
  })
})
