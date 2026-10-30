import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { userFixture, categoryFixture, channelFixture, voiceUserFixture } from '../test-fixtures.fixture'
import VoiceStage from './VoiceStage.vue'

const layout = vi.hoisted(() => ({ compact: { value: true } }))
vi.mock('../composables/useCompactLayout', async () => {
  const { ref: vueRef } = await import('vue')
  layout.compact = vueRef(true)
  return { useCompactLayout: () => ({ isCompact: layout.compact, navOpen: vueRef(false), openNav: () => {}, closeNav: () => {} }) }
})
vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ joinVoiceChannel: vi.fn(), leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn(), applyAudioSettings: vi.fn(), toggleCamera: vi.fn() })
}))

function connectedWith(others: string[]) {
  useAuthStore().user = userFixture({ id: 'me', username: 'me', display_name: 'Me', role: 'user' })
  useChatStore().categories = [categoryFixture({ id: 'cat', name: 'Room', channels: [channelFixture({ id: 'v1', name: 'Lounge', type: 'voice' })] })]
  const voice = useVoiceStore()
  voice.setChannel('v1')
  voice.channelUsers = {
    v1: Object.fromEntries(others.map(id => [id, voiceUserFixture({ id, username: id, display_name: id })]))
  }
}

beforeEach(() => {
  localStorage.clear()
  setLocale('en')
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve([]) })))
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Talk controls on a phone', () => {
  const bar = (w: ReturnType<typeof mount>) => w.get('[data-testid="talk-controls"]')

  it('stay visible while alone in the call, with no hint needed', async () => {
    connectedWith([])
    const w = mount(VoiceStage, { props: { channelId: 'v1' }, attachTo: document.body })
    await nextTick()
    vi.advanceTimersByTime(10_000)
    await nextTick()
    expect(bar(w).attributes('data-visible')).toBe('true')
    expect(w.find('[data-testid="talk-tap-hint"]').exists()).toBe(false)
    w.unmount()
  })

  it('hide with others in the call, and say how to bring them back', async () => {
    connectedWith(['alice'])
    const w = mount(VoiceStage, { props: { channelId: 'v1' }, attachTo: document.body })
    await nextTick()
    expect(w.find('[data-testid="talk-tap-hint"]').exists()).toBe(false)
    vi.advanceTimersByTime(3100)
    await nextTick()
    expect(bar(w).attributes('data-visible')).toBe('false')
    expect(w.get('[data-testid="talk-tap-hint"]').text()).toBe('Tap the screen to show the controls')
    await w.get('main').trigger('pointerdown')
    expect(bar(w).attributes('data-visible')).toBe('true')
    expect(w.find('[data-testid="talk-tap-hint"]').exists()).toBe(false)
    w.unmount()
  })

  it('a tap on a tile only brings hidden controls back; it does not open the profile', async () => {
    connectedWith(['alice'])
    const profile = vi.spyOn(useChatStore(), 'openUserProfile').mockResolvedValue(undefined)
    const w = mount(VoiceStage, { props: { channelId: 'v1' }, attachTo: document.body })
    await nextTick()
    vi.advanceTimersByTime(3100)
    await nextTick()
    const tile = () => w.get<HTMLElement>('[data-user-id="alice"]').element
    tile().dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' }))
    tile().click()
    await nextTick()
    expect(bar(w).attributes('data-visible')).toBe('true')
    expect(profile).not.toHaveBeenCalled()
    tile().dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' }))
    tile().click()
    expect(profile).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('sit in a row of their own, not over the tiles, while the Talk chat is open', async () => {
    connectedWith(['alice'])
    const w = mount(VoiceStage, { props: { channelId: 'v1', showChat: true }, attachTo: document.body })
    await nextTick()
    expect(bar(w).element.parentElement?.className).not.toContain('absolute')
    await w.setProps({ showChat: false })
    expect(bar(w).element.parentElement?.className).toContain('absolute')
    w.unmount()
  })

  it('keep hiding on a wide screen with others, and say that the mouse brings them back', async () => {
    layout.compact.value = false
    connectedWith(['alice'])
    const w = mount(VoiceStage, { props: { channelId: 'v1' }, attachTo: document.body })
    await nextTick()
    vi.advanceTimersByTime(3100)
    await nextTick()
    expect(bar(w).attributes('data-visible')).toBe('false')
    expect(w.get('[data-testid="talk-tap-hint"]').text()).toBe('Move the mouse to show the controls')
    w.unmount()
    layout.compact.value = true
  })

  it('stay on a wide screen while alone in the call', async () => {
    layout.compact.value = false
    connectedWith([])
    const w = mount(VoiceStage, { props: { channelId: 'v1' }, attachTo: document.body })
    await nextTick()
    vi.advanceTimersByTime(10_000)
    await nextTick()
    expect(bar(w).attributes('data-visible')).toBe('true')
    expect(w.find('[data-testid="talk-tap-hint"]').exists()).toBe(false)
    w.unmount()
    layout.compact.value = true
  })
})
