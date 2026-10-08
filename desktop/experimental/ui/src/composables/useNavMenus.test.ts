import { categoryFixture, channelFixture, readStateFixture, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import type { ContextMenuItem, MenuItemId } from '../components/menuTypes'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { nextTick } from 'vue'
import { pendingConfirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { api } from '../lib/api'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { buildChannelItems, buildCategoryItems, buildMemberItems, buildSidebarItems, channelLink, mentionMember, useMenuState, notifyLevelOf } from './useNavMenus'

vi.mock('../lib/api', async original => ({ ...(await original<typeof import('../lib/api')>()), api: vi.fn() }))

function menuItem(items: ContextMenuItem[], id: MenuItemId) { return required(items.find(item => item.id === id)) }
function checkedItem(items: ContextMenuItem[], id: MenuItemId) {
  const item = menuItem(items, id)
  if (item.type !== 'radio' && item.type !== 'checkbox') throw new Error('Expected selection menu item')
  return item
}
function sliderItem(items: ContextMenuItem[], id: MenuItemId) {
  const item = menuItem(items, id)
  if (item.type !== 'slider') throw new Error('Expected slider menu item')
  return item
}

const textCh = channelFixture({ id: 't1', type: 'text', name: 'general' })
const voiceCh = channelFixture({ id: 'v1', type: 'voice', name: 'Runde' })
const ids = (items: ContextMenuItem[]) => items.filter(i => i.id).map(i => i.id)

let auth: ReturnType<typeof useAuthStore>, chat: ReturnType<typeof useChatStore>, voice: ReturnType<typeof useVoiceStore>
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  auth = useAuthStore()
  chat = useChatStore()
  voice = useVoiceStore()
  auth.user = userFixture({ id: 'me', role: 'user' })
  vi.mocked(api).mockReset()
})
afterEach(() => { vi.unstubAllGlobals(); pendingConfirm.value = null })

describe('menu actions and state', () => {
  it('retains notification preferences when a legacy store has no helper and defaults to all', () => {
    const preferences = { notificationLevel: chat.notificationLevel, readStates: { t1: readStateFixture({ notify_level: 'mute' }) } }
    Reflect.deleteProperty(preferences, 'notificationLevel')
    expect(notifyLevelOf(preferences, 't1')).toBe('mute')
    expect(notifyLevelOf(preferences, 'missing')).toBe('all')
    vi.stubGlobal('window', undefined)
    expect(channelLink(textCh)).toBe('/c/t1')
    vi.unstubAllGlobals()
  })
  it('opens after a tick and refreshes dynamically built items', async () => {
    const menu = useMenuState()
    let label = 'first'
    expect(menu.items.value).toEqual([])
    const event = new MouseEvent('contextmenu', { clientX: 10, clientY: 20, cancelable: true })
    const show = menu.show(event, refresh => [{ label, action: refresh }])
    expect(menu.state.open).toBe(false)
    await show
    expect(event.defaultPrevented).toBe(true)
    expect(menu.state).toMatchObject({ open: true, x: 10, y: 20 })
    expect(menu.items.value[0]?.label).toBe('first')
    label = 'second'
    required(menu.items.value[0]?.action)()
    expect(menu.items.value[0]?.label).toBe('second')
    menu.state.open = false
    expect(menu.items.value).toEqual([])
    menu.state.open = true
    menu.state.build = null
    expect(menu.items.value).toEqual([])
  })

  it('marks read, copies each channel link and reports unavailable clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    chat.markChannelRead = vi.fn(async () => {})
    await required(menuItem(buildChannelItems(textCh), 'mark-read').action)()
    expect(chat.markChannelRead).toHaveBeenCalledWith('t1')
    await required(menuItem(buildChannelItems(textCh), 'copy-link').action)()
    expect(writeText).toHaveBeenLastCalledWith(`${window.location.origin}/c/t1`)
    await required(menuItem(buildChannelItems(voiceCh), 'copy-link').action)()
    expect(writeText).toHaveBeenLastCalledWith(`${window.location.origin}/v/v1`)
    expect(channelLink(voiceCh)).toContain('/v/v1')
    writeText.mockRejectedValue(new Error('denied'))
    await required(menuItem(buildChannelItems(textCh), 'copy-link').action)()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
  })

  it('dispatches all admin edit/delete and create callbacks and tolerates absent callbacks', () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    const onEdit = vi.fn(), onDelete = vi.fn(), onDuplicate = vi.fn()
    const channels = buildChannelItems(textCh, { onEdit, onDelete, onDuplicate })
    for (const id of ['edit', 'delete', 'duplicate']) required(menuItem(channels, id).action)()
    expect(onEdit).toHaveBeenCalledWith(textCh)
    expect(onDelete).toHaveBeenCalledWith(textCh)
    const cat = categoryFixture()
    const categories = buildCategoryItems(cat, { onEdit, onDelete })
    for (const id of ['edit', 'delete']) required(menuItem(categories, id).action)()
    expect(onDelete).toHaveBeenCalledWith(cat)
    for (const list of [buildChannelItems(textCh), buildCategoryItems(cat), buildSidebarItems()]) {
      for (const item of list.filter(item => ['edit', 'delete', 'duplicate', 'create-channel', 'create-category', 'collapse-all', 'expand-all'].includes(String(item.id)))) item.action?.()
    }
  })

  it('mentions in the active composer or clipboard and ignores nameless members', async () => {
    const member = userFixture({ id: 'u2', username: 'anna' })
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    chat.insertMention = vi.fn()
    chat.activeChannel = textCh
    await mentionMember(member)
    expect(chat.insertMention).toHaveBeenCalledWith('anna')
    chat.activeChannel = null
    await mentionMember(member)
    expect(writeText).toHaveBeenCalledWith('@anna')
    await mentionMember({ id: 'nameless' })
    expect(writeText).toHaveBeenCalledOnce()
  })

  it('opens profiles and exposes mute/unmute controls, refresh optional', async () => {
    const member = userFixture({ id: 'u2', username: 'anna' })
    chat.openUserProfile = vi.fn()
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { u2: voiceUserFixture({ id: 'u2' }) } }
    const list = buildMemberItems(member)
    await required(menuItem(list, 'profile').action)()
    expect(chat.openUserProfile).toHaveBeenCalledWith(member)
    await required(menuItem(list, 'mention').action)()
    sliderItem(list, 'volume').onInput?.(50)
    required(menuItem(list, 'local-mute').action)()
    expect(voice.isUserLocalMuted('u2')).toBe(true)
    required(menuItem(buildMemberItems(member), 'local-mute').action)()
    expect(voice.isUserLocalMuted('u2')).toBe(false)
    auth.user = null
    expect(menuItem(buildMemberItems({ id: 'nameless' }), 'mention').disabled).toBe(true)
  })

  it('requires confirmation for moderation, posts exact routes and surfaces both error forms', async () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    const member = userFixture({ id: 'u2', username: 'anna', display_name: '' })
    voice.channelUsers = { v1: { u2: voiceUserFixture({ id: 'u2' }) } }
    const items = buildMemberItems(member)
    const rejected = required(menuItem(items, 'kick').action)()
    required(pendingConfirm.value).resolve(false)
    await rejected
    expect(api).not.toHaveBeenCalled()
    for (const [id, path, error] of [['kick', 'kick', null], ['revoke', 'sessions/revoke', new Error('offline')], ['disable', 'disable', 'failed']] as const) {
      if (error === null) vi.mocked(api).mockResolvedValueOnce(null)
      else vi.mocked(api).mockRejectedValueOnce(error)
      const request = required(menuItem(items, id).action)()
      required(pendingConfirm.value).resolve(true)
      await request
      expect(api).toHaveBeenLastCalledWith(`/api/admin/users/u2/${path}`, { method: 'POST' })
    }
    expect(useToastStore().toasts.some(toast => toast.text === 'offline')).toBe(true)
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
    await nextTick()
  })

  it('allows moderation of a member without display or user name', async () => {
    auth.user = userFixture({ role: 'admin' })
    const request = required(menuItem(buildMemberItems({ id: 'u2' }), 'disable').action)()
    expect(pendingConfirm.value?.title).toContain('Disable')
    required(pendingConfirm.value).resolve(false)
    await request
  })
})

describe('channel menu', () => {
  it('gives members read, link and notification options only', () => {
    expect(ids(buildChannelItems(textCh))).toEqual(['mark-read', 'copy-link', 'notify-all', 'notify-mentions', 'notify-mute'])
  })

  it('adds edit, duplicate and delete for admins', () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    const onDuplicate = vi.fn()
    const list = buildChannelItems(textCh, { onDuplicate })
    expect(ids(list).slice(-3)).toEqual(['edit', 'duplicate', 'delete'])
    expect(menuItem(list, 'duplicate').label).toBe('Duplicate channel')
    required(menuItem(list, 'duplicate').action)()
    expect(onDuplicate).toHaveBeenCalledWith(textCh)
    expect(ids(buildChannelItems(voiceCh)).slice(-3)).toEqual(['edit', 'duplicate', 'delete'])
  })

  it('voice channels have a chat, so read state and notifications too', () => {
    expect(ids(buildChannelItems(voiceCh))).toEqual(['mark-read', 'copy-link', 'notify-all', 'notify-mentions', 'notify-mute'])
    chat.readStates = { [voiceCh.id]: readStateFixture({ channel_id: voiceCh.id, unread_count: 1, mention_count: 0 }) }
    chat.setNotificationLevel = vi.fn()
    const list = buildChannelItems(voiceCh)
    expect(menuItem(list, 'mark-read').disabled).toBe(false)
    required(menuItem(list, 'notify-mute').action)()
    expect(chat.setNotificationLevel).toHaveBeenCalledWith(voiceCh.id, 'mute')
  })

  it('marks the current notification level and sets a new one', () => {
    chat.readStates = { t1: readStateFixture({ channel_id: 't1', unread_count: 2, mention_count: 0, notify_level: 'mentions' }) }
    chat.setNotificationLevel = vi.fn()
    const list = buildChannelItems(textCh)
    expect(checkedItem(list, 'notify-mentions').checked).toBe(true)
    expect(checkedItem(list, 'notify-all').checked).toBe(false)
    expect(menuItem(list, 'mark-read').disabled).toBe(false)
    required(menuItem(list, 'notify-mute').action)()
    expect(chat.setNotificationLevel).toHaveBeenCalledWith('t1', 'mute')
  })

  it('disables mark-read without unread messages', () => {
    expect(menuItem(buildChannelItems(textCh), 'mark-read').disabled).toBe(true)
  })
})

describe('category menu', () => {
  const cat = categoryFixture({ id: 'c1', name: 'Text', channels: [textCh] })
  it('members get mark all read and collapse / expand all', () => {
    expect(ids(buildCategoryItems(cat))).toEqual(['mark-all-read', 'collapse-all', 'expand-all'])
  })
  it('admins can also create, edit and delete', () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    expect(ids(buildCategoryItems(cat))).toEqual([
      'mark-all-read', 'collapse-all', 'expand-all', 'create-channel', 'create-category', 'edit', 'delete'
    ])
  })
  it('collapse and expand all are disabled when there is nothing to do', () => {
    const onCollapseAll = vi.fn()
    const onExpandAll = vi.fn()
    let list = buildCategoryItems(cat, { onCollapseAll, onExpandAll, allCollapsed: true, noneCollapsed: false })
    expect(menuItem(list, 'collapse-all').disabled).toBe(true)
    expect(menuItem(list, 'expand-all').disabled).toBe(false)
    required(menuItem(list, 'expand-all').action)()
    expect(onExpandAll).toHaveBeenCalled()
    list = buildCategoryItems(cat, { onCollapseAll, onExpandAll, allCollapsed: false, noneCollapsed: true })
    expect(menuItem(list, 'collapse-all').disabled).toBe(false)
    expect(menuItem(list, 'expand-all').disabled).toBe(true)
    required(menuItem(list, 'collapse-all').action)()
    expect(onCollapseAll).toHaveBeenCalled()
  })
  it('mark all read covers the chats of voice channels too', async () => {
    chat.readStates = { v1: readStateFixture({ channel_id: 'v1', unread_count: 2, mention_count: 0 }) }
    chat.markChannelRead = vi.fn(async () => {})
    const item = menuItem(buildCategoryItems(categoryFixture({ id: 'c2', name: 'Talks', channels: [textCh, voiceCh] })), 'mark-all-read')
    expect(item.disabled).toBe(false)
    await required(item.action)()
    expect(vi.mocked(chat.markChannelRead).mock.calls.map(c => c[0])).toEqual(['t1', 'v1'])
  })
  it('passes the category to the create handlers', () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    const onCreateChannel = vi.fn()
    const onCreateCategory = vi.fn()
    const list = buildCategoryItems(cat, { onCreateChannel, onCreateCategory })
    required(menuItem(list, 'create-channel').action)()
    required(menuItem(list, 'create-category').action)()
    expect(onCreateChannel).toHaveBeenCalledWith(cat)
    expect(onCreateCategory).toHaveBeenCalledWith(cat)
  })
})

describe('channel list background menu', () => {
  it('is empty for members, so the browser menu stays', () => {
    expect(buildSidebarItems()).toEqual([])
  })
  it('lets admins create a channel or a category', () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    const onCreateChannel = vi.fn()
    const onCreateCategory = vi.fn()
    const list = buildSidebarItems({ onCreateChannel, onCreateCategory })
    expect(ids(list)).toEqual(['create-channel', 'create-category'])
    expect(list.map(i => i.label)).toEqual(['Create channel', 'Create category'])
    required(required(list[0]).action)()
    required(required(list[1]).action)()
    expect(onCreateChannel).toHaveBeenCalled()
    expect(onCreateCategory).toHaveBeenCalled()
  })
})

describe('member menu', () => {
  const other = userFixture({ id: 'u2', username: 'anna', display_name: 'Anna' })

  it('members get profile and mention', () => {
    expect(ids(buildMemberItems(other))).toEqual(['profile', 'mention'])
  })

  it('adds volume and local mute for someone in my call', () => {
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { me: voiceUserFixture({ id: 'me' }), u2: voiceUserFixture({ id: 'u2' }) } }
    voice.setUserVolume = vi.fn()
    voice.getUserVolume = vi.fn(() => 150)
    const refresh = vi.fn()
    const list = buildMemberItems(other, { refresh })
    expect(ids(list)).toEqual(['profile', 'mention', 'volume', 'local-mute', 'camera-hide'])
    const slider = sliderItem(list, 'volume')
    expect(slider.value).toBe(150)
    expect(slider.max).toBe(200)
    required(slider.onInput)(80)
    expect(voice.setUserVolume).toHaveBeenCalledWith('u2', 80)
    expect(refresh).toHaveBeenCalled()
  })

  it('toggles hiding someone\'s camera, unless all cameras are off', () => {
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { me: voiceUserFixture({ id: 'me' }), u2: voiceUserFixture({ id: 'u2' }) } }
    const hide = menuItem(buildMemberItems(other), 'camera-hide')
    required(hide.action)()
    expect(voice.isCameraHidden('u2')).toBe(true)
    expect(menuItem(buildMemberItems(other), 'camera-hide').label).toBe('Show camera')
    voice.setAllCamerasOff(true)
    expect(ids(buildMemberItems(other))).not.toContain('camera-hide')
  })

  it('never offers volume for myself', () => {
    voice.currentChannelId = 'v1'
    voice.channelUsers = { v1: { me: voiceUserFixture({ id: 'me' }) } }
    expect(ids(buildMemberItems(userFixture({ id: 'me', username: 'me' })))).toEqual(['profile', 'mention'])
  })

  it('admins get kick (only in voice), end sessions and disable for others', () => {
    auth.user = userFixture({ id: 'me', role: 'admin' })
    expect(ids(buildMemberItems(other))).toEqual(['profile', 'mention', 'revoke', 'disable'])
    voice.channelUsers = { v9: { u2: voiceUserFixture({ id: 'u2' }) } }
    expect(ids(buildMemberItems(other))).toEqual(['profile', 'mention', 'kick', 'revoke', 'disable'])
    expect(ids(buildMemberItems(userFixture({ id: 'me', username: 'me' })))).toEqual(['profile', 'mention'])
  })
})
