

import { userFixture, channelFixture, categoryFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import UserBar from './UserBar.vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'
import { api } from '../lib/api'

vi.mock('../lib/api', async (orig) => ({ ...(await orig()), api: vi.fn() }))

const leave = vi.fn()
const share = vi.fn()
vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ leaveVoiceChannel: leave, startScreenShare: share, stopScreenShare: vi.fn() })
}))

function setup() {
  const voice = useVoiceStore()
  const chat = useChatStore()
  const auth = useAuthStore()
  auth.user = userFixture({ bio: '', status_text: '', created_at: '2026-01-01T00:00:00Z', username: 'member',  id: "00000000-0000-4000-8000-0000000003e8", display_name: 'Herzog', role: 'admin', locale: 'de' })
  chat.categories = [categoryFixture({ created_at: '2026-01-01T00:00:00Z',  id: "00000000-0000-4000-8000-0000000003f2", name: 'Kat', channels: [channelFixture({ topic: '', created_at: '2026-01-01T00:00:00Z', sort_order: 0, category_id: null,  id: "00000000-0000-4000-8000-0000000003f3", name: 'Lounge', type: 'voice' })] })]
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
    expect(w.find('[aria-label="Sprachverbindung"]')!.exists()).toBe(false)
    expect(w.find('[data-testid="toggle-mute"]')!.exists()).toBe(false)
    expect(w.find('[data-testid="toggle-deafen"]')!.exists()).toBe(false)
    // the ⋯ menu (with the audio settings) and the presence button are always there
    expect(w.find('[data-testid="account-menu-button"]')!.exists()).toBe(true)
    expect(w.find('[data-testid="presence-button"]')!.exists()).toBe(true)
  })

  it('shows the panel with channel, ping colour and actions while connected', async () => {
    const { w, voice } = setup()
    voice.setChannel("00000000-0000-4000-8000-0000000003f3")
    voice.recordPing(4)
    await nextTick()
    expect(w.find('[data-testid="voice-panel-open"]')!.text()).toBe('Verbunden · Lounge')
    const ping = w.find('[data-testid="voice-panel-ping"]')!
    expect(ping.text()).toMatch(/^4 ms/)
    expect(ping.attributes('data-tone')).toBe('good')
    voice.recordPing(186)
    await nextTick()
    expect(ping.attributes('data-tone')).toBe('warn')
    voice.recordPing(260)
    await nextTick()
    expect(ping.attributes('data-tone')).toBe('bad')

    // Clicking the ping opens the connection details.
    await ping.trigger('click')
    expect(voice.showStatsModal).toBe(true)
    await w.find('[data-testid="voice-panel-share"]')!.trigger('click')
    expect(share).toHaveBeenCalled()
    await w.find('[data-testid="voice-panel-leave"]')!.trigger('click')
    expect(leave).toHaveBeenCalled()
  })

  it('clicking the status opens the Talk', async () => {
    const { w, voice } = setup()
    voice.setChannel("00000000-0000-4000-8000-0000000003f3")
    voice.activeView = 'chat'
    await nextTick()
    await w.find('[data-testid="voice-panel-open"]')!.trigger('click')
    expect(voice.activeView).toBe('voice')
  })

  it('mute and deafen toggles swap label and aria-pressed', async () => {
    const { w, voice } = setup()
    voice.setChannel("00000000-0000-4000-8000-0000000003f3")
    await nextTick()
    const mute = w.find('[data-testid="toggle-mute"]')!
    expect(mute.attributes('aria-label')).toBe('Stummschalten')
    expect(mute.attributes('aria-pressed')).toBe('false')
    await mute.trigger('click')
    expect(voice.isMuted).toBe(true)
    expect(mute.attributes('aria-label')).toBe('Stummschaltung aufheben')
    expect(mute.attributes('aria-pressed')).toBe('true')
    const deafen = w.find('[data-testid="toggle-deafen"]')!
    await deafen.trigger('click')
    expect(voice.isDeafened).toBe(true)
    expect(deafen.attributes('aria-label')).toBe('Ton wieder an')
  })

  it('speaks English with an English account', async () => {
    const { w, voice } = setup()
    setLocale('en')
    voice.setChannel("00000000-0000-4000-8000-0000000003f3")
    await nextTick()
    expect(w.find('[data-testid="voice-panel-open"]')!.text()).toBe('Connected · Lounge')
    expect(w.find('[data-testid="toggle-mute"]')!.attributes('aria-label')).toBe('Mute')
  })
})

describe('UserBar presence and status', () => {
  it('shows the status text, else the live presence', async () => {
    const { w, chat, auth } = setup()
    chat.presenceById = { '00000000-0000-4000-8000-0000000003e8': 'dnd' }
    await nextTick()
    expect(w.find('[data-testid="own-subline"]')!.text()).toBe('Nicht stören')
    auth.user = userFixture({ ...auth.user, status_text: 'zockt grad' })
    await nextTick()
    expect(w.find('[data-testid="own-subline"]')!.text()).toBe('zockt grad')
  })

  it('opens the presence menu from the avatar and offers no offline', async () => {
    const { w } = setup()
    await w.find('[data-testid="presence-button"]')!.trigger('click')
    const menu = w.find('[data-testid="presence-menu"]')!
    expect(menu.exists()).toBe(true)
    const choices = menu.findAll('[data-presence]').map(b => b.attributes('data-presence'))
    expect(choices).toEqual(['online', 'away', 'dnd', 'focus'])
  })

  it('saves a chosen presence', async () => {
    const { w, auth } = setup()
    vi.mocked(api).mockResolvedValueOnce({ ...auth.user, presence: 'focus' })
    await w.find('[data-testid="presence-button"]')!.trigger('click')
    await w.find('[data-presence="focus"]')!.trigger('click')
    await flushPromises()
    expect(api).toHaveBeenCalledWith('/api/users/me/presence', { decode: expect.any(Function),  method: 'PUT', json: { presence: 'focus' } })
    expect(auth.user!.presence).toBe('focus')
    expect(w.find('[data-testid="presence-menu"]')!.exists()).toBe(false)
  })

  it('opens the own profile from the name', async () => {
    const { w, chat } = setup()
    vi.mocked(api).mockResolvedValueOnce({ id: "00000000-0000-4000-8000-0000000003e8" })
    await w.find('[data-testid="own-profile-button"]')!.trigger('click')
    expect(chat.selectedUserProfile?.id).toBe("00000000-0000-4000-8000-0000000003e8")
  })
})
