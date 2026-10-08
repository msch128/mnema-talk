import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './stores/auth'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import { useToastStore } from './stores/toast'
import * as router from './lib/router'
import { channelFixture, categoryFixture, messageFixture, userFixture, requireValue } from './test-fixtures.fixture'
import App from './App.vue'

const rtc = vi.hoisted(() => ({ resumeVoiceSession: vi.fn(async () => false), resumeRemoteAudio: vi.fn(), leaveVoiceChannel: vi.fn(), rejoinAfterReconnect: vi.fn() }))
vi.mock('./composables/useWebRTC', () => ({ useWebRTC: () => rtc }))
const channels = [channelFixture({ id: 'ch1' }), channelFixture({ id: 'ch2' }), channelFixture({ id: 'ch3' }), channelFixture({ id: 'v1', type: 'voice' }), channelFixture({ id: 'v2', type: 'voice' })]
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  sessionStorage.clear()
  window.history.replaceState(null, '', '/')
  router.navigate('/', { replace: true })
  setActivePinia(createPinia())
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals() })

async function mountApp({ authenticated = true, admin = false, path = '/c/ch1' } = {}) {
  router.navigate(path, { replace: true })
  const auth = useAuthStore(), chat = useChatStore(), voice = useVoiceStore(), toasts = useToastStore()
  auth.checkAuth = vi.fn(async () => { auth.user = authenticated ? userFixture({ role: admin ? 'admin' : 'user' }) : null; return authenticated })
  chat.fetchChannels = vi.fn(async () => { chat.categories = [categoryFixture({ channels })] })
  chat.fetchMembers = vi.fn(async () => {})
  chat.fetchReadState = vi.fn(async () => ({}))
  chat.initWebSocket = vi.fn()
  chat.closeWebSocket = vi.fn()
  chat.selectChannel = vi.fn(async channel => { chat.activeChannel = channel })
  chat.jumpToMessage = vi.fn(async () => true)
  chat.openThread = vi.fn(async root => { chat.activeThread = typeof root === 'string' ? messageFixture({ id: root }) : root })
  chat.closeThread = vi.fn(() => { chat.activeThread = null })
  const w = mount(App, { shallow: true })
  wrapper = w
  await flushPromises()
  return { w, auth, chat, voice, toasts }
}
async function go(path: string) { router.navigate(path); await flushPromises() }

 describe('App routing and session lifecycle', () => {
  it.each([false, true])('keeps the shared ChatArea mounted in desktop for admin=%s and limits root setup to admins', async admin => {
    vi.stubGlobal('isTauri', true)
    const { w, chat } = await mountApp({ admin })
    expect(w.findComponent({ name: 'ChatArea' }).exists()).toBe(true)
    expect(w.findComponent({ name: 'UpdateBanner' }).exists()).toBe(false)
    const control = w.findComponent({ name: 'NativeTrustControl' })
    expect(control.exists()).toBe(admin)
    if (admin) {
      chat.fetchMessages = vi.fn(async () => true)
      control.vm.$emit('saved'); await flushPromises()
      expect(chat.fetchMessages).toHaveBeenCalledExactlyOnceWith('ch1')
      chat.activeChannel = null; await flushPromises()
      control.vm.$emit('saved'); await flushPromises()
      expect(chat.fetchMessages).toHaveBeenCalledOnce()
      expect(w.findComponent({ name: 'NativeTrustControl' }).exists()).toBe(false)
    }
  })
  it('keeps the server reload banner available in the browser', async () => {
    vi.stubGlobal('isTauri', false)
    const { w } = await mountApp()
    expect(w.findComponent({ name: 'UpdateBanner' }).exists()).toBe(true)
  })
  it('selects text channels, opens/closes threads, and repairs dead message and thread links', async () => {
    const { chat, toasts } = await mountApp()
    await go('/c/ch2/t/root')
    expect(chat.activeThread?.id).toBe('root')
    await go('/c/ch2/t/root')
    expect(chat.openThread).toHaveBeenCalledTimes(1)
    await go('/c/ch2')
    expect(chat.activeThread).toBeNull()
    await go('/c/ch2/m/found')
    expect(chat.jumpToMessage).toHaveBeenCalledWith('found')
    chat.jumpToMessage = vi.fn(async () => false)
    await go('/c/ch2/m/dead')
    expect(window.location.pathname).toBe('/c/ch2')
    chat.openThread = vi.fn(async root => { chat.activeThread = { id: typeof root === 'string' ? root : root.id } })
    const error = vi.spyOn(toasts, 'error')
    await go('/c/ch2/t/dead')
    expect(chat.activeThread).toBeNull()
    expect(error).toHaveBeenCalled()
    expect(window.location.pathname).toBe('/c/ch2')
  })
  it('starts on a deep linked talk chat and waits for channels before routing', async () => {
    const { chat, voice, w } = await mountApp({ path: '/v/v1/chat' })
    expect(w.findComponent({ name: 'VoiceChatPanel' }).exists()).toBe(true)
    await go('/c/ch1')
    voice.activeView = 'voice'
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch1')
    chat.categories = []
    await go('/c/ch2')
    expect(chat.activeChannel?.id).toBe('ch1')
    chat.categories = [categoryFixture({ channels })]
    await go('/v/v2/chat')
    expect(chat.activeChannel?.id).toBe('v2')
  })
  it('guards admin routes and repairs missing or incorrectly typed channels', async () => {
    const { toasts, w } = await mountApp()
    const error = vi.spyOn(toasts, 'error')
    await go('/admin/users')
    expect(w.findComponent({ name: 'AdminDashboard' }).exists()).toBe(false)
    expect(error).toHaveBeenCalled()
    await go('/c/missing')
    expect(window.location.pathname).toBe('/c/ch1')
    await go('/v/missing')
    expect(window.location.pathname).toBe('/c/ch1')
    await go('/v/ch2')
    expect(window.location.pathname).toBe('/c/ch2')
    await go('/c/v1/m/message')
    expect(window.location.pathname).toBe('/v/v1/chat/m/message')
  })
  it('previews talks, remembers their chat, joins the current talk and reacts to mentions', async () => {
    const { chat, voice, w } = await mountApp()
    await go('/v/v1')
    expect(w.findComponent({ name: 'VoiceStage' }).props('channelId')).toBe('v1')
    expect(w.findComponent({ name: 'VoiceChatPanel' }).exists()).toBe(false)
    chat.pendingMention = 'member'
    await flushPromises()
    expect(window.location.pathname).toBe('/v/v1/chat')
    w.findComponent({ name: 'VoiceChatPanel' }).vm.$emit('close')
    await flushPromises()
    expect(window.location.pathname).toBe('/v/v1')
    w.findComponent({ name: 'VoiceStage' }).vm.$emit('update:showChat', true)
    await flushPromises()
    await go('/v/v2')
    expect(window.location.pathname).toBe('/v/v2/chat')
    await go('/v/v2')
    expect(w.findComponent({ name: 'VoiceChatPanel' }).exists()).toBe(false)
    voice.currentChannelId = 'v2'
    await flushPromises()
    w.findComponent({ name: 'VoiceStage' }).vm.$emit('join', 'v1')
    await flushPromises()
    expect(w.findComponent({ name: 'VoiceStage' }).props('channelId')).toBe('v1')
    w.findComponent({ name: 'VoiceStage' }).vm.$emit('join', 'v2')
    await flushPromises()
    expect(w.findComponent({ name: 'VoiceStage' }).props('channelId')).toBe('v2')
    chat.jumpToMessage = vi.fn(async () => false)
    await go('/v/v2/chat/m/dead')
    expect(window.location.pathname).toBe('/v/v2/chat')
    await go('/v/v2/chat/m/found')
    expect(chat.jumpToMessage).toHaveBeenCalledWith('found')
    await go('/c/ch1')
    expect(voice.activeView).toBe('chat')
  })
  it('renders optional dialogs and invokes their user actions', async () => {
    const { chat, voice, w } = await mountApp({ admin: true })
    chat.activeThread = messageFixture()
    chat.showMemberList = false
    await flushPromises()
    chat.showMemberList = true
    voice.showStatsModal = true
    voice.showAudioSettings = true
    voice.showScreenShareModal = true
    voice.audioBlocked = true
    voice.isConnected = true
    chat.selectedUserProfile = userFixture()
    const closeProfile = vi.spyOn(chat, 'closeUserProfile')
    const mention = vi.spyOn(chat, 'insertMention')
    const screenClose = vi.spyOn(voice, 'closeScreenShareModal')
    await flushPromises()
    await w.find('button').trigger('click')
    expect(rtc.resumeRemoteAudio).toHaveBeenCalledOnce()
    w.findComponent({ name: 'ConnectionStatsModal' }).vm.$emit('close')
    w.findComponent({ name: 'AudioSettingsModal' }).vm.$emit('close')
    w.findComponent({ name: 'ScreenShareModal' }).vm.$emit('close')
    w.findComponent({ name: 'UserProfileModal' }).vm.$emit('mention', 'member')
    w.findComponent({ name: 'UserProfileModal' }).vm.$emit('close')
    expect(closeProfile).toHaveBeenCalledOnce()
    expect(mention).toHaveBeenCalledWith('member')
    expect(screenClose).toHaveBeenCalledOnce()
    expect(voice.showStatsModal).toBe(false)
    expect(voice.showAudioSettings).toBe(false)
    for (const name of ['Sidebar', 'UserBar']) {
      w.findComponent({ name }).vm.$emit('open-legal')
      await flushPromises()
      expect(w.findComponent({ name: 'LegalModal' }).exists()).toBe(true)
      w.findComponent({ name: 'LegalModal' }).vm.$emit('close')
      await flushPromises()
      w.findComponent({ name }).vm.$emit('open-admin')
      await flushPromises()
      expect(w.findComponent({ name: 'AdminDashboard' }).props('initialTab')).toBe('users')
      await go('/c/ch1')
    }
  })
  it('closes deep linked admin to current text/thread/talk state and uses app history when available', async () => {
    const { chat, voice, w } = await mountApp({ admin: true })
    const back = vi.spyOn(window.history, 'back').mockImplementation(() => {})
    await go('/admin/system')
    w.findComponent({ name: 'AdminDashboard' }).vm.$emit('close')
    expect(back).toHaveBeenCalledOnce()
    for (const path of ['/c/ch1', '/c/ch1/t/root', '/v/v1', '/v/v1/chat']) {
      await go(path)
      router.navigate('/admin/system', { replace: true })
      window.history.replaceState({ idx: 0 }, '', '/admin/system')
      await flushPromises()
      w.findComponent({ name: 'AdminDashboard' }).vm.$emit('close')
      await flushPromises()
      expect(window.location.pathname).toBe(path)
    }
    voice.activeView = 'chat'
    chat.activeChannel = null
    chat.activeThread = null
    await flushPromises()
    router.navigate('/admin/system', { replace: true })
    window.history.replaceState({ idx: 0 }, '', '/admin/system')
    await flushPromises()
    w.findComponent({ name: 'AdminDashboard' }).vm.$emit('close')
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch1')
  })
  it('initializes at root, chooses voice when no text channel exists, and preserves current route state', async () => {
    const { chat, voice } = await mountApp({ path: '/' })
    expect(window.location.pathname).toBe('/c/ch1')
    chat.activeThread = messageFixture({ id: 'root' })
    await flushPromises()
    await go('/')
    expect(window.location.pathname).toBe('/c/ch1/t/root')
    await go('/v/v1')
    await go('/')
    expect(window.location.pathname).toBe('/v/v1')
    voice.activeView = 'chat'
    chat.activeThread = null
    chat.activeChannel = null
    chat.categories = [categoryFixture({ channels: [requireValue(channels[3])] })]
    await flushPromises()
    await go('/')
    expect(window.location.pathname).toBe('/v/v1')
  })
  it('resumes voice once per login, reconnects, leaves on logout, and consumes the saved route', async () => {
    const { auth, chat, voice, w } = await mountApp()
    chat.isConnected = true
    await flushPromises()
    chat.isConnected = false
    await flushPromises()
    chat.isConnected = true
    chat.reconnectCount++
    await flushPromises()
    expect(rtc.resumeVoiceSession).toHaveBeenCalledOnce()
    expect(rtc.rejoinAfterReconnect).toHaveBeenCalledOnce()
    voice.currentChannelId = 'v1'
    auth.user = null
    await flushPromises()
    expect(rtc.leaveVoiceChannel).toHaveBeenCalledOnce()
    expect(chat.closeWebSocket).toHaveBeenCalledOnce()
    expect(w.findComponent({ name: 'LoginModal' }).exists()).toBe(true)
    sessionStorage.setItem('mnema_pending_route', '/c/ch3')
    auth.user = userFixture()
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch3')
    auth.user = null
    voice.currentChannelId = null
    await flushPromises()
    sessionStorage.clear()
    auth.user = userFixture()
    await flushPromises()
    expect(chat.initWebSocket).toHaveBeenCalledTimes(3)
    chat.isConnected = false
    await flushPromises()
    rtc.resumeVoiceSession.mockRejectedValueOnce(new Error('network unavailable'))
    chat.isConnected = true
    await flushPromises()
    expect(rtc.resumeVoiceSession).toHaveBeenCalledTimes(2)
  })
  it('does not open admin for ordinary members and ignores routes after logout', async () => {
    const { w, auth, chat } = await mountApp()
    w.findComponent({ name: 'UserBar' }).vm.$emit('open-admin')
    await flushPromises()
    expect(w.findComponent({ name: 'AdminDashboard' }).exists()).toBe(false)
    auth.user = null
    await flushPromises()
    await go('/c/ch3')
    expect(chat.activeChannel?.id).toBe('ch1')
  })
  it('keeps loading on transient auth errors and caps exponential retry delay', async () => {
    vi.useFakeTimers()
    const auth = useAuthStore()
    const answers: Array<boolean | null> = [null, null, null, null, null, null, false]
    auth.checkAuth = vi.fn(async () => answers.length ? answers.splice(0, 1)[0] ?? null : false)
    wrapper = mount(App, { shallow: true })
    await flushPromises()
    expect(wrapper.findComponent({ name: 'LoginModal' }).exists()).toBe(false)
    expect(wrapper.text()).toContain('neuer Versuch')
    for (const delay of [1000, 2000, 4000, 8000, 15000, 15000]) await vi.advanceTimersByTimeAsync(delay)
    await flushPromises()
    expect(auth.checkAuth).toHaveBeenCalledTimes(7)
    expect(wrapper.findComponent({ name: 'LoginModal' }).exists()).toBe(true)
  })
  it('handles a known signed-out startup without fetching community data', async () => {
    const { chat, w } = await mountApp({ authenticated: false })
    expect(w.findComponent({ name: 'LoginModal' }).exists()).toBe(true)
    expect(chat.fetchChannels).not.toHaveBeenCalled()
  })
  it.each([false, true])('retires auth polling when App unmounts (pending response: %s)', async pending => {
    vi.useFakeTimers()
    const auth = useAuthStore()
    let resolve!: (value: null) => void
    auth.checkAuth = vi.fn(() => pending ? new Promise<null>(r => { resolve = r }) : Promise.resolve(null))
    wrapper = mount(App, { shallow: true })
    await flushPromises()
    wrapper.unmount(); wrapper = undefined
    if (pending) resolve(null)
    await flushPromises()
    await vi.advanceTimersByTimeAsync(20000)
    expect(auth.checkAuth).toHaveBeenCalledOnce()
  })
  it('does not reopen a socket or continue routing after pending initialization unmounts', async () => {
    const auth = useAuthStore(), chat = useChatStore()
    let resolve!: () => void
    auth.checkAuth = vi.fn(async () => { auth.user = userFixture(); return true })
    chat.fetchChannels = vi.fn(() => new Promise<void>(r => { resolve = r }))
    chat.fetchMembers = vi.fn(async () => {})
    chat.fetchReadState = vi.fn(async () => ({}))
    chat.initWebSocket = vi.fn()
    chat.jumpToMessage = vi.fn(async () => true)
    router.navigate('/c/ch1/m/old')
    wrapper = mount(App, { shallow: true })
    await flushPromises()
    wrapper.unmount(); wrapper = undefined
    resolve(); await flushPromises()
    expect(chat.initWebSocket).not.toHaveBeenCalled()
    expect(chat.jumpToMessage).not.toHaveBeenCalled()
    expect(chat.fetchChannels).toHaveBeenCalledOnce()
  })
  it('does not start a voice-message jump after the rendering tick outlives App', async () => {
    const { chat, w } = await mountApp({ path: '/v/v1/chat' })
    router.navigate('/v/v1/chat/m/old')
    await Promise.resolve()
    w.unmount(); wrapper = undefined
    await flushPromises()
    expect(chat.jumpToMessage).not.toHaveBeenCalled()
  })
  it('ignores old routes after newer channel, message and thread loads finish', async () => {
    const { chat } = await mountApp()
    const selects: Array<() => void> = []
    chat.selectChannel = vi.fn(channel => new Promise<void>(resolve => { selects.push(() => { chat.activeChannel = channel; resolve() }) }))
    await go('/c/ch2/t/old')
    await go('/c/ch3')
    requireValue(selects[1])()
    await flushPromises()
    requireValue(selects[0])()
    await flushPromises()
    expect(chat.openThread).not.toHaveBeenCalled()
    chat.selectChannel = vi.fn(async channel => { chat.activeChannel = channel })
    let done: (() => void) | undefined
    chat.jumpToMessage = vi.fn(() => new Promise<boolean>(resolve => { done = () => resolve(false) }))
    await go('/c/ch2/m/old')
    await go('/c/ch3')
    requireValue(done)()
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch3')
    chat.openThread = vi.fn(() => new Promise<void>(resolve => { done = resolve }))
    await go('/c/ch2/t/old')
    await go('/c/ch3')
    requireValue(done)()
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch3')
    await go('/v/v1/chat/m/old')
    await go('/c/ch3')
    requireValue(done)()
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch3')
    chat.selectChannel = vi.fn(channel => new Promise<void>(resolve => { done = () => { chat.activeChannel = channel; resolve() } }))
    await go('/v/v2/chat')
    const oldVoiceDone = requireValue(done)
    await go('/c/ch1')
    requireValue(done)()
    await flushPromises()
    oldVoiceDone()
    await flushPromises()
    expect(window.location.pathname).toBe('/c/ch1')
  })
})
