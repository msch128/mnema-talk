import type { VueWrapper } from '@vue/test-utils'
import type { ComponentPublicInstance } from 'vue'
import { userFixture, categoryFixture, channelFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import VoiceStage from './VoiceStage.vue'

const rtc = vi.hoisted(() => ({
  joinVoiceChannel: vi.fn(),
  leaveVoiceChannel: vi.fn(),
  startScreenShare: vi.fn(),
  stopScreenShare: vi.fn(),
  applyAudioSettings: vi.fn(),
  toggleCamera: vi.fn()
}))
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => rtc }))

const getUserMedia = vi.fn()

function seed() {
  useAuthStore().user = userFixture({ id: 'me', username: 'me', display_name: 'Me', role: 'user' })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: 'cat', name: 'Room', channels: [
    channelFixture({ id: 'v1', name: 'Lounge', type: 'voice' }),
    channelFixture({ id: 'v2', name: 'Other', type: 'voice' })
  ] })]
  const voice = useVoiceStore()
  voice.channelUsers = { v1: { a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }) } }
  return { chat, voice }
}

// Unmounted after each test: a mounted stage listens on the document (F key).
let mounted: VueWrapper<ComponentPublicInstance>[] = []

beforeEach(() => {
  localStorage.clear()
  setLocale('en')
  setActivePinia(createPinia())
  Object.values(rtc).forEach(fn => fn.mockReset())
  getUserMedia.mockReset()
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } })
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve([]) })))
  // Every element is 800 x 450 css px, as the stage would be.
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(800)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(450)
})
afterEach(() => {
  for (const w of mounted) {
    try { w.unmount() } catch { /* already unmounted */ }
  }
  mounted = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('what the stage tells the server about a camera on it', () => {
  it('reports the size of the stage for the person whose camera it shows, whatever screen is watched', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.serverCameraLayers = 3
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' })
    } }
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', new MediaStream())
    // Bob's screen is the one being watched; Alice's camera is on the stage.
    voice.handleMediaState({ channel_id: 'v1', user_id: 'b', screen: true })
    voice.watchScreen('b')
    voice.focusCamera('a')
    const w = mount(VoiceStage, { props: { channelId: 'v1' }, attachTo: document.body })
    mounted.push(w)
    await flushPromises()

    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:a')
    const sizes = voice.cameraSizes['a'] ?? {}
    expect(Object.entries(sizes).filter(([key]) => key.startsWith('stage')).map(([, px]) => px)).toEqual([450])
    expect(voice.cameraSizes['b']).toBeUndefined()
  })
})
