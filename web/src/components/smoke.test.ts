import type { Message } from '../types/domain'
import { requireValue } from '../test-fixtures.fixture'
import { messageFixture, userFixture, categoryFixture, channelFixture, voiceUserFixture } from '../test-fixtures.fixture'
// Renders every dialog and main view in both languages: no Vue warnings, and
// no raw i18n key (like "admin.title") leaking into the page.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'

import LoginModal from './LoginModal.vue'
import LegalModal from './LegalModal.vue'
import CreateChannelModal from './CreateChannelModal.vue'
import ConnectionStatsModal from './ConnectionStatsModal.vue'
import AdminDashboard from './AdminDashboard.vue'
import UserProfileModal from './UserProfileModal.vue'
import AudioSettingsModal from './AudioSettingsModal.vue'
import ChatArea from './ChatArea.vue'
import ThreadSidebar from './ThreadSidebar.vue'
import MemberList from './MemberList.vue'
import Sidebar from './Sidebar.vue'
import VoiceStage from './VoiceStage.vue'

vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({
    joinVoiceChannel: vi.fn(), leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn(),
    refreshAudioDevices: vi.fn(), startMicTest: vi.fn(), applyAudioSettings: vi.fn(), stopMicTest: vi.fn(),
    resumeRemoteAudio: vi.fn()
  })
}))

const RAW_KEY = /\b(common|chat|admin|profile|legal|talk|mention|emoji|activity|audio|voice|stats|login|channel|sidebar|thread|members|account|connection|errors|menu|role|presence|user|media|resize|app)\.[A-Za-z][A-Za-z.]*\b/

const msg = (id: string, extra: Partial<Message> = {}) => (messageFixture({
  id, channel_id: 'c1', user_id: 'u1', display_name: 'Herzog', username: 'herzog', content: 'Hallo **Welt**',
  created_at: '2026-01-01T10:00:00Z', attachments: [], reactions: [{ emoji: '👍', count: 1, users: ['u1'] }], ...extra
}))

function seed() {
  const auth = useAuthStore()
  auth.user = userFixture({ id: 'u1', username: 'herzog', display_name: 'Herzog', role: 'admin', locale: 'de' })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: 'cat', name: 'Allgemein', channels: [channelFixture({ id: 'c1', name: 'allgemein', type: 'text', topic: 'Hi' }), channelFixture({ id: 'v1', name: 'Lounge', type: 'voice' })] })]
  chat.activeChannel = requireValue(requireValue(chat.categories[0]).channels[0])
  chat.members = [userFixture({ id: 'u1', username: 'herzog', display_name: 'Herzog', role: 'admin' }), userFixture({ id: 'u2', username: 'zoe', display_name: 'Zoe', role: 'user' })]
  chat.presenceById = { u1: 'online' }
  chat.messages = [msg('m1', { reply_count: 2, is_edited: true }), msg('m2', { reply_to_id: 'm1', reply_to: { deleted: false, id: 'm1', content: 'x', display_name: 'Herzog' } })]
  chat.activeThread = msg('m1')
  chat.threadReplies = [msg('r1')]
  const voice = useVoiceStore()
  voice.setChannel('v1')
  voice.channelUsers = { v1: { u1: voiceUserFixture({ id: 'u1', display_name: 'Herzog', username: 'herzog', role: 'admin' }) } }
  return { auth, chat, voice }
}

const views = {
  LoginModal: () => mount(LoginModal, { attachTo: document.body }),
  LegalModal: () => mount(LegalModal, { attachTo: document.body }),
  CreateChannelModal: () => mount(CreateChannelModal, { attachTo: document.body }),
  ConnectionStatsModal: () => mount(ConnectionStatsModal, { attachTo: document.body }),
  AdminDashboard: () => mount(AdminDashboard, { attachTo: document.body }),
  UserProfileModal: () => mount(UserProfileModal, { attachTo: document.body, props: { user: userFixture({ id: 'u1', username: 'herzog', display_name: 'Herzog', role: 'admin' }) } }),
  AudioSettingsModal: () => mount(AudioSettingsModal, { attachTo: document.body }),
  ChatArea: () => mount(ChatArea, { attachTo: document.body }),
  ThreadSidebar: () => mount(ThreadSidebar, { attachTo: document.body }),
  MemberList: () => mount(MemberList, { attachTo: document.body }),
  Sidebar: () => mount(Sidebar, { attachTo: document.body }),
  VoiceStage: () => mount(VoiceStage, { attachTo: document.body })
}

// Text of all nodes, joined with spaces so adjacent elements don't glue words together.
function visibleText(root: Node) {
  const out: string[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  while (walker.nextNode()) out.push((walker.currentNode.textContent ?? '').trim())
  return out.filter(Boolean).join(' ')
}

let warn = vi.fn<typeof console.warn>()
beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve([]) })))
  warn = vi.spyOn(console, 'warn').mockImplementation(async () => {})
})
afterEach(() => {
  vi.unstubAllGlobals()
  warn.mockRestore()
  document.body.innerHTML = ''
})

describe.each(['de', 'en'] as const)('smoke render (%s)', locale => {
  for (const [name, make] of Object.entries(views)) {
    it(`${name} renders without warnings or raw keys`, async () => {
      setLocale(locale)
      seed()
      const w = make()
      await nextTick()
      await nextTick()
      const text = visibleText(document.body)
      const attrs = [...document.body.querySelectorAll('[aria-label],[placeholder],[title],[alt]')]
        .flatMap(el => ['aria-label', 'placeholder', 'title', 'alt'].map(a => el.getAttribute(a)).filter((text): text is string => text !== null))
      expect(warn.mock.calls.filter(c => String(c[0]).includes('[Vue warn]'))).toEqual([])
      expect(text.match(RAW_KEY)).toBeNull()
      expect(attrs.filter(a => RAW_KEY.test(a))).toEqual([])
      w.unmount()
    })
  }
})
