import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { buildChannelItems, buildCategoryItems, buildMemberItems } from './useNavMenus'

const textCh = { id: 't1', type: 'text', name: 'general' }
const voiceCh = { id: 'v1', type: 'voice', name: 'Runde' }
const ids = items => items.filter(i => i.id).map(i => i.id)

let auth, chat, voice
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  auth = useAuthStore()
  chat = useChatStore()
  voice = useVoiceStore()
  auth.user = { id: 'me', role: 'user' }
})

describe('channel menu', () => {
  it('gives members read, link and notification options only', () => {
    expect(ids(buildChannelItems(textCh))).toEqual(['mark-read', 'copy-link', 'notify-all', 'notify-mentions', 'notify-mute'])
  })

  it('adds edit and delete for admins', () => {
    auth.user = { id: 'me', role: 'admin' }
    expect(ids(buildChannelItems(textCh)).slice(-2)).toEqual(['edit', 'delete'])
  })

  it('voice channels have no read state or notifications', () => {
    expect(ids(buildChannelItems(voiceCh))).toEqual(['copy-link'])
  })

  it('marks the current notification level and sets a new one', () => {
    chat.readStates = { t1: { channel_id: 't1', unread_count: 2, mention_count: 0, notify_level: 'mentions' } }
    chat.setNotificationLevel = vi.fn()
    const list = buildChannelItems(textCh)
    expect(list.find(i => i.id === 'notify-mentions').checked).toBe(true)
    expect(list.find(i => i.id === 'notify-all').checked).toBe(false)
    expect(list.find(i => i.id === 'mark-read').disabled).toBe(false)
    list.find(i => i.id === 'notify-mute').action()
    expect(chat.setNotificationLevel).toHaveBeenCalledWith('t1', 'mute')
  })

  it('disables mark-read without unread messages', () => {
    expect(buildChannelItems(textCh).find(i => i.id === 'mark-read').disabled).toBe(true)
  })
})

describe('category menu', () => {
  const cat = { id: 'c1', name: 'Text', channels: [textCh] }
  it('members only get mark all read', () => {
    expect(ids(buildCategoryItems(cat))).toEqual(['mark-all-read'])
  })
  it('admins can edit and delete', () => {
    auth.user = { id: 'me', role: 'admin' }
    expect(ids(buildCategoryItems(cat))).toEqual(['mark-all-read', 'edit', 'delete'])
  })
})

describe('member menu', () => {
  const other = { id: 'u2', username: 'anna', display_name: 'Anna' }

  it('members get profile and mention', () => {
    expect(ids(buildMemberItems(other))).toEqual(['profile', 'mention'])
  })

  it('adds volume and local mute for someone in my call', () => {
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { me: { id: 'me' }, u2: { id: 'u2' } } }
    voice.setUserVolume = vi.fn()
    voice.getUserVolume = vi.fn(() => 150)
    const refresh = vi.fn()
    const list = buildMemberItems(other, { refresh })
    expect(ids(list)).toEqual(['profile', 'mention', 'volume', 'local-mute', 'camera-hide'])
    const slider = list.find(i => i.id === 'volume')
    expect(slider.value).toBe(150)
    expect(slider.max).toBe(200)
    slider.onInput(80)
    expect(voice.setUserVolume).toHaveBeenCalledWith('u2', 80)
    expect(refresh).toHaveBeenCalled()
  })

  it('toggles hiding someone\'s camera, unless all cameras are off', () => {
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { me: { id: 'me' }, u2: { id: 'u2' } } }
    const hide = buildMemberItems(other).find(i => i.id === 'camera-hide')
    hide.action()
    expect(voice.isCameraHidden('u2')).toBe(true)
    expect(buildMemberItems(other).find(i => i.id === 'camera-hide').label).toBe('Show camera')
    voice.setAllCamerasOff(true)
    expect(ids(buildMemberItems(other))).not.toContain('camera-hide')
  })

  it('never offers volume for myself', () => {
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { me: { id: 'me' } } }
    expect(ids(buildMemberItems({ id: 'me', username: 'me' }))).toEqual(['profile', 'mention'])
  })

  it('admins get kick (only in voice), end sessions and disable for others', () => {
    auth.user = { id: 'me', role: 'admin' }
    expect(ids(buildMemberItems(other))).toEqual(['profile', 'mention', 'revoke', 'disable'])
    voice.channelUsers = { v9: { u2: { id: 'u2' } } }
    expect(ids(buildMemberItems(other))).toEqual(['profile', 'mention', 'kick', 'revoke', 'disable'])
    expect(ids(buildMemberItems({ id: 'me', username: 'me' }))).toEqual(['profile', 'mention'])
  })
})
