import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { categoryFixture, channelFixture, fixtureId, readStateFixture, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import type { AvatarSize, LivePresence, ResizePanel } from './presentationTypes'
import SidebarCategoryHeader from './SidebarCategoryHeader.vue'
import SidebarChannelRow from './SidebarChannelRow.vue'
import SidebarDragGhost from './SidebarDragGhost.vue'
import ResizeHandle from './ResizeHandle.vue'
import MemberList from './MemberList.vue'
import MemberRow from './MemberRow.vue'
import PresenceDot from './PresenceDot.vue'
import UserAvatar from './UserAvatar.vue'
import MuteMarks from './MuteMarks.vue'
import ContextMenu from './ContextMenu.vue'
vi.mock('../lib/api', async original => ({ ...(await original<typeof import('../lib/api')>()), api: vi.fn() }))
const wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T { wrappers.push(w); return w }
beforeEach(() => setActivePinia(createPinia()))
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); document.body.innerHTML = ''; vi.restoreAllMocks() })
it('category supports every admin action and rejects member moves', async () => {
  const w = keep(mount(SidebarCategoryHeader, { props: { category: categoryFixture(), admin: true, hintId: 'hint', dragging: true, flash: true, indicator: 'inside' } }))
  const toggle = w.find('button')
  expect(toggle.attributes('aria-expanded')).toBe('true'); expect(toggle.attributes('aria-describedby')).toBe('hint')
  await toggle.trigger('click'); await toggle.trigger('keydown', { key: 'ArrowUp', altKey: true }); await toggle.trigger('keydown', { key: 'ArrowDown', altKey: true })
  await toggle.trigger('keydown', { key: 'F10', shiftKey: true }); await toggle.trigger('keydown', { key: 'ContextMenu' }); await w.trigger('contextmenu'); await w.trigger('pointerdown')
  await w.findAll('button')[1]!.trigger('click'); await w.findAll('button')[2]!.trigger('click')
  expect(w.emitted('move')).toEqual([[-1], [1]]); expect(w.emitted('menu')).toHaveLength(3)
  for (const event of ['toggle', 'create-channel', 'delete', 'drag-start']) expect(w.emitted(event)).toHaveLength(1)
  for (const indicator of ['top', 'bottom', ''] as const) { await w.setProps({ indicator, collapsed: true, admin: false }); expect(w.find('[data-drop-indicator]').exists()).toBe(indicator !== '') }
  await toggle.trigger('keydown', { key: 'ArrowUp', altKey: true }); expect(w.emitted('move')).toHaveLength(2)
  expect(toggle.attributes('aria-expanded')).toBe('false'); expect(toggle.attributes('aria-describedby')).toBeUndefined()
})
it('channel supports every mouse and keyboard event and rejects member moves', async () => {
  const w = keep(mount(SidebarChannelRow, { props: { channel: channelFixture(), admin: true, hintId: 'hint', flash: true, dragging: true, indicator: 'top' } }))
  const row = w.find('[data-drop="channel"]')
  await row.trigger('click'); await row.trigger('contextmenu'); await row.trigger('pointerdown')
  for (const key of ['Enter', ' ', 'ContextMenu']) await row.trigger('keydown', { key })
  await row.trigger('keydown', { key: 'F10', shiftKey: true })
  for (const key of ['ArrowUp', 'ArrowDown']) await row.trigger('keydown', { key, altKey: true })
  await w.find('button').trigger('click')
  expect(w.emitted('open')).toHaveLength(3); expect(w.emitted('menu')).toHaveLength(3); expect(w.emitted('move')).toEqual([[-1], [1]])
  expect(w.emitted('delete')).toHaveLength(1); expect(w.emitted('drag-start')).toHaveLength(1)
  await w.setProps({ admin: false, indicator: 'bottom' }); await row.trigger('keydown', { key: 'ArrowDown', altKey: true }); expect(w.emitted('move')).toHaveLength(2)
  await w.setProps({ indicator: '' }); expect(w.find('[data-drop-indicator]').exists()).toBe(false)
})
it('badges reflect text and voice reading, unread and mention state', async () => {
  const chat = useChatStore(); const voice = useVoiceStore(); const channel = channelFixture()
  const w = keep(mount(SidebarChannelRow, { props: { channel } }))
  expect(w.find('[data-testid="channel-unread"]').exists()).toBe(false)
  chat.readStates[channel.id] = readStateFixture({ unread_count: 4 }); await nextTick(); expect(w.find('[data-testid="channel-unread"]').text()).toBe('4')
  chat.readStates[channel.id] = readStateFixture({ unread_count: 4, mention_count: 2 }); await nextTick(); expect(w.find('[data-testid="channel-mentions"]').text()).toBe('2')
  chat.activeChannel = channel; await nextTick(); expect(w.find('[data-testid="channel-mentions"]').exists()).toBe(false)
  await w.setProps({ channel: { ...channel, type: 'voice' } }); voice.currentChannelId = channel.id; voice.activeView = 'voice'; voice.roomStartedAt[channel.id] = '2026-01-01T00:00:00Z'
  chat.voiceChatReading = false; await nextTick(); expect(w.find('[data-testid="channel-mentions"]').exists()).toBe(true)
  chat.voiceChatReading = true; await nextTick(); expect(w.find('[data-testid="channel-mentions"]').exists()).toBe(false)
  chat.activeChannel = null; voice.currentChannelId = null; await nextTick(); expect(w.find('[data-testid="channel-mentions"]').exists()).toBe(true)
})
it('voice user rows show live and role badges and support all accessible actions', async () => {
  const voice = useVoiceStore(); const channel = channelFixture({ type: 'voice' })
  const live = voiceUserFixture({ id: fixtureId(10), display_name: 'Live' }); const admin = voiceUserFixture({ id: fixtureId(11), role: 'admin' }); const ordinary = voiceUserFixture({ id: fixtureId(12) })
  voice.channelUsers[channel.id] = { [live.id]: live, [admin.id]: admin, [ordinary.id]: ordinary }; voice.mediaState[live.id] = { channel_id: channel.id, screen: true, camera: false }
  const w = keep(mount(SidebarChannelRow, { props: { channel, admin: true } })); expect(w.find('[data-testid="sidebar-live-badge"]').exists()).toBe(true)
  const member = w.find('[data-voice-user]'); await member.trigger('click'); await member.trigger('keydown', { key: 'Enter' }); await member.trigger('contextmenu'); await member.trigger('keydown', { key: 'F10', shiftKey: true }); await member.trigger('keydown', { key: 'ContextMenu' })
  expect(w.emitted('voice-user-click')).toEqual([[live], [live]]); expect(w.emitted('member-menu')).toHaveLength(3)
})
it('member rows display status precedence and emit profile and menu gestures', async () => {
  const chat = useChatStore(); const member = userFixture({ role: 'admin', display_name: 'Ada' }); const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(async () => {})
  chat.presenceById[member.id] = 'away'; const w = keep(mount(MemberRow, { props: { member } })); expect(w.text()).toContain('Abwesend')
  await w.trigger('click'); await w.trigger('keydown', { key: 'Enter' }); await w.trigger('contextmenu'); await w.trigger('keydown', { key: 'F10', shiftKey: true }); await w.trigger('keydown', { key: 'ContextMenu' })
  expect(open).toHaveBeenCalledTimes(2); expect(w.emitted('menu')).toHaveLength(3)
  await w.setProps({ member: { ...member, role: 'user', display_name: '', status_text: 'Building' } }); expect(w.text()).toContain('member'); expect(w.text()).toContain('Building')
  await w.setProps({ voiceChannel: 'Lounge' }); expect(w.text()).toContain('Lounge'); expect(w.text()).not.toContain('Building')
  chat.presenceById[member.id] = 'offline'; await w.setProps({ voiceChannel: '' }); expect(w.text()).not.toContain('Building')
  await w.setProps({ member: userFixture({ status_text: '' }) }); chat.presenceById[member.id] = 'online'; await nextTick(); expect(w.find('.text-xs.text-mnema-tertiary').exists()).toBe(false)
})
it('member list groups roles and resolves known and missing voice channel names', async () => {
  const chat = useChatStore(); const voice = useVoiceStore(); const admin = userFixture({ id: fixtureId(20), role: 'admin' }); const online = userFixture({ id: fixtureId(21) }); const unknown = userFixture({ id: fixtureId(22) }); const offline = userFixture({ id: fixtureId(23) })
  chat.members = [admin, online, unknown, offline]; chat.presenceById = { [admin.id]: 'online', [online.id]: 'online', [unknown.id]: 'online' }
  const catChannel = channelFixture({ id: fixtureId(30), name: 'Categorized' }); const uncat = channelFixture({ id: fixtureId(31), name: 'Uncategorized' })
  chat.categories = [categoryFixture(), categoryFixture({ id: fixtureId(40), channels: [catChannel] })]; chat.uncategorized = [uncat]
  voice.channelUsers = { [catChannel.id]: { [admin.id]: voiceUserFixture(admin) }, [uncat.id]: { [online.id]: voiceUserFixture(online) }, [fixtureId(32)]: { [unknown.id]: voiceUserFixture(unknown) } }
  const w = keep(mount(MemberList, { global: { stubs: { ContextMenu: { template: '<button data-menu-close @click="$emit(\'update:modelValue\', false)">Close</button>' } } } })); expect(w.findAllComponents(MemberRow)).toHaveLength(4)
  expect(w.text()).toContain('Categorized'); expect(w.text()).toContain('Uncategorized'); expect(w.text()).toContain('Sprachkanal'); for (const row of w.findAllComponents(MemberRow)) await row.trigger('contextmenu')
  await w.findComponent(ContextMenu).find('button').trigger('click')
  voice.channelUsers = {}; await nextTick(); expect(w.findAllComponents(MemberRow).every(row => row.props('voiceChannel') === '')).toBe(true)
  chat.members = []; await nextTick(); expect(w.findAll('section')).toHaveLength(0)
})
describe('small presentation contracts', () => {
  it.each<AvatarSize>(['xxs', 'xs', 'sm', 'md', 'lg', 'xl'])('renders avatar size %s', size => {
    const w = keep(mount(UserAvatar, { props: { size, user: { display_name: 'Ada' }, showStatus: true, isOnline: true, isSpeaking: true } })); expect(w.text()).toBe('A'); expect(w.find('[data-status]').attributes('data-status')).toBe('online'); expect(w.findComponent(PresenceDot).props('size')).toBe(size === 'xl' ? 12 : size === 'lg' ? 10 : 8)
  })
  it('handles absent users, initials and avatar alt text', async () => {
    const w = keep(mount(UserAvatar)); expect(w.text()).toBe('?'); await w.setProps({ user: null, showStatus: true }); expect(w.text()).toBe('?'); expect(w.find('[data-status]').attributes('data-status')).toBe('offline')
    await w.setProps({ user: { username: 'bea' } }); expect(w.text()).toBe('B')
    await w.setProps({ user: { username: 'bea', avatar_url: '/api/media/avatar' }, status: 'focus' }); expect(w.find('img').attributes('alt')).toBe('Profilbild'); expect(w.find('[data-status]').attributes('data-status')).toBe('focus')
    await w.setProps({ user: { display_name: 'Bea', avatar_url: '/api/media/avatar' } }); expect(w.find('img').attributes('alt')).toBe('Bea')
  })
  it.each<LivePresence>(['online', 'away', 'dnd', 'focus', 'offline'])('renders distinct presence shape %s', async status => {
    const w = keep(mount(PresenceDot, { props: { status, ringClass: 'ring', size: 12 } })); expect(w.attributes('data-status')).toBe(status); expect(w.attributes('style')).toContain('16px'); expect(w.find('svg').exists()).toBe(true)
    await w.setProps({ ring: false }); expect(w.attributes('style')).toContain('12px'); expect(w.classes()).not.toContain('ring')
  })
  it.each([[false, false, 0], [true, false, 1], [false, true, 2], [true, true, 2]] as const)('renders mute=%s deafen=%s', (muted, deafened, count) => { const w = keep(mount(MuteMarks, { props: { muted, deafened, size: 16 } })); expect(w.findAll('[role="img"]')).toHaveLength(count) })
  it('positions mouse and touch drag ghosts for all item types', async () => {
    const w = keep(mount(SidebarDragGhost, { props: { item: { kind: 'channel', name: 'Text', type: 'text' }, x: 20, y: 100 }, global: { stubs: { Teleport: true } } })); expect(w.find('[data-testid="drag-ghost"]').attributes('style')).toContain('34px, 110px')
    await w.setProps({ touch: true, item: { kind: 'channel', name: 'Voice', type: 'voice' } }); expect(w.find('[data-testid="drag-ghost"]').attributes('style')).toContain('-4px, 44px'); await w.setProps({ item: { kind: 'category', name: 'Category' } }); expect(w.text()).toBe('Category')
  })
  it.each(['left', 'right', 'top', 'bottom'] as const)('forwards accessible resize events on %s edge', async side => {
    const panel: ResizePanel = { name: 'panel', side, axis: side === 'top' || side === 'bottom' ? 'y' : 'x', min: 100, maxNow: 500, width: 240, defaultWidth: 240, dragging: true, startDrag: vi.fn(), onKeydown: vi.fn(), reset: vi.fn() }
    const w = keep(mount(ResizeHandle, { props: { panel, label: 'Resize panel' } })); expect(w.attributes('aria-orientation')).toBe(panel.axis === 'y' ? 'horizontal' : 'vertical'); expect(w.attributes('aria-valuenow')).toBe('240')
    await w.trigger('pointerdown'); await w.trigger('keydown', { key: 'ArrowRight' }); await w.trigger('dblclick'); expect(panel.startDrag).toHaveBeenCalledTimes(1); expect(panel.onKeydown).toHaveBeenCalledTimes(1); expect(panel.reset).toHaveBeenCalledTimes(1)
    await w.setProps({ panel: { ...panel, dragging: false }, label: '' }); expect(w.attributes('aria-label')).toBe('Breite anpassen')
  })
})

it('falls back to medium dimensions for malformed runtime avatar size', () => {
  // Vue runtime props may originate from JavaScript callers despite the TS union.
  const options = { props: { user: { username: 'bea' }, size: 'unexpected' } }
  const w = keep(Reflect.apply(mount, undefined, [UserAvatar, options]))
  expect(w.find('.rounded-full').classes()).toContain('w-10')
})
