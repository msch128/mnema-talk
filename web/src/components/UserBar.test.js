import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import UserBar from './UserBar.vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

const leave = vi.fn()
const share = vi.fn()
vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ leaveVoiceChannel: leave, startScreenShare: share, stopScreenShare: vi.fn() })
}))

function setup() {
  const voice = useVoiceStore()
  const chat = useChatStore()
  const auth = useAuthStore()
  auth.user = { id: 'u1', display_name: 'Herzog', role: 'admin', locale: 'de' }
  chat.categories = [{ id: 'c', name: 'Kat', channels: [{ id: 'v1', name: 'Lounge', type: 'voice' }] }]
  return { voice, chat, auth, w: mount(UserBar) }
}

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  leave.mockClear()
  share.mockClear()
})

describe('UserBar voice status panel', () => {
  it('shows no panel and no mute/deafen toggles when not connected', () => {
    const { w } = setup()
    expect(w.find('[aria-label="Sprachverbindung"]').exists()).toBe(false)
    expect(w.find('[data-testid="toggle-mute"]').exists()).toBe(false)
    expect(w.find('[data-testid="toggle-deafen"]').exists()).toBe(false)
    // audio settings and the ⋯ menu are always there
    expect(w.find('[aria-label="Audio-Einstellungen"]').exists()).toBe(true)
    expect(w.find('[data-testid="account-menu-button"]').exists()).toBe(true)
  })

  it('shows the panel with channel, ping colour and actions while connected', async () => {
    const { w, voice } = setup()
    voice.setChannel('v1')
    voice.recordPing(4)
    await nextTick()
    expect(w.find('[data-testid="voice-panel-open"]').text()).toBe('Verbunden · Lounge')
    const ping = w.find('[data-testid="voice-panel-ping"]')
    expect(ping.text()).toBe('4 ms')
    expect(ping.attributes('data-tone')).toBe('good')
    voice.recordPing(186)
    await nextTick()
    expect(ping.attributes('data-tone')).toBe('warn')
    voice.recordPing(260)
    await nextTick()
    expect(ping.attributes('data-tone')).toBe('bad')

    await w.find('[data-testid="voice-panel-details"]').trigger('click')
    expect(voice.showStatsModal).toBe(true)
    await w.find('[data-testid="voice-panel-share"]').trigger('click')
    expect(share).toHaveBeenCalled()
    await w.find('[data-testid="voice-panel-leave"]').trigger('click')
    expect(leave).toHaveBeenCalled()
  })

  it('clicking the status opens the Tafelrunde', async () => {
    const { w, voice } = setup()
    voice.setChannel('v1')
    voice.activeView = 'chat'
    await nextTick()
    await w.find('[data-testid="voice-panel-open"]').trigger('click')
    expect(voice.activeView).toBe('voice')
  })

  it('mute and deafen toggles swap label and aria-pressed', async () => {
    const { w, voice } = setup()
    voice.setChannel('v1')
    await nextTick()
    const mute = w.find('[data-testid="toggle-mute"]')
    expect(mute.attributes('aria-label')).toBe('Stummschalten')
    expect(mute.attributes('aria-pressed')).toBe('false')
    await mute.trigger('click')
    expect(voice.isMuted).toBe(true)
    expect(mute.attributes('aria-label')).toBe('Stummschaltung aufheben')
    expect(mute.attributes('aria-pressed')).toBe('true')
    const deafen = w.find('[data-testid="toggle-deafen"]')
    await deafen.trigger('click')
    expect(voice.isDeafened).toBe(true)
    expect(deafen.attributes('aria-label')).toBe('Ton wieder an')
  })

  it('speaks English with an English account', async () => {
    const { w, voice } = setup()
    setLocale('en')
    voice.setChannel('v1')
    await nextTick()
    expect(w.find('[data-testid="voice-panel-open"]').text()).toBe('Connected · Lounge')
    expect(w.find('[data-testid="toggle-mute"]').attributes('aria-label')).toBe('Mute')
  })
})
