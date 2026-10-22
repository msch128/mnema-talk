import { describe, it, expect, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { channelDraftKey, clearAllDrafts, setDraft } from '../lib/drafts'
import { usePermissionsStore } from '../stores/permissions'
import { channelFixture, readStateFixture, userFixture } from '../test-fixtures.fixture'
import SidebarCategoryHeader from './SidebarCategoryHeader.vue'
import SidebarChannelRow from './SidebarChannelRow.vue'
import SidebarServerMenu from './SidebarServerMenu.vue'

beforeEach(() => setActivePinia(createPinia()))

describe('category header activity', () => {
  const category = { id: 'k1', name: 'Music' }
  const header = (props: Record<string, unknown>) => mount(SidebarCategoryHeader, { props: { category, ...props } })

  it('shows a mention count or an unread dot only while collapsed', () => {
    expect(header({ collapsed: true, mentions: 4, unreadChannels: 2 }).find('[data-testid="category-mentions"]').text()).toBe('4')
    expect(header({ collapsed: true, mentions: 4, unreadChannels: 2 }).find('[data-testid="category-unread"]').exists()).toBe(false)
    expect(header({ collapsed: true, unreadChannels: 2 }).find('[data-testid="category-unread"]').exists()).toBe(true)
    expect(header({ collapsed: false, mentions: 4, unreadChannels: 2 }).find('[data-testid="category-mentions"]').exists()).toBe(false)
    expect(header({ collapsed: true }).find('[data-testid="category-unread"]').exists()).toBe(false)
  })
})

describe('muted channel row', () => {
  it('is dimmed, marked and shows no plain unread badge, but keeps its mentions', async () => {
    const chat = useChatStore()
    const channel = channelFixture({ id: 'c1', name: 'quiet' })
    chat.readStates = { c1: readStateFixture({ channel_id: 'c1', unread_count: 4, mention_count: 0, notify_level: 'mute' }) }
    const row = mount(SidebarChannelRow, { props: { channel } })
    expect(row.find('[data-testid="channel-muted"]').exists()).toBe(true)
    expect(row.find('[data-testid="channel-unread"]').exists()).toBe(false)
    expect(row.find('[data-drop="channel"]').classes()).toContain('opacity-60')

    chat.readStates = { c1: readStateFixture({ channel_id: 'c1', unread_count: 4, mention_count: 2, notify_level: 'mute' }) }
    await nextTick()
    expect(row.find('[data-testid="channel-mentions"]').text()).toBe('2')

    chat.readStates = { c1: readStateFixture({ channel_id: 'c1', unread_count: 4, mention_count: 0, notify_level: 'all' }) }
    await nextTick()
    expect(row.find('[data-testid="channel-muted"]').exists()).toBe(false)
    expect(row.find('[data-testid="channel-unread"]').text()).toBe('4')
  })
})

describe('channel row draft marker', () => {
  it('marks a channel with an unsent message, except the one being read', async () => {
    localStorage.clear()
    clearAllDrafts()
    const auth = useAuthStore()
    auth.user = userFixture({ id: 'me' })
    const channel = channelFixture({ id: 'c1', name: 'general' })
    const row = mount(SidebarChannelRow, { props: { channel } })
    expect(row.find('[data-testid="channel-draft"]').exists()).toBe(false)

    setDraft('me', channelDraftKey('c1'), { text: 'later', reply: null })
    await nextTick()
    expect(row.find('[data-testid="channel-draft"]').text()).toBe('Entwurf')

    useChatStore().activeChannel = channel
    await nextTick()
    expect(row.find('[data-testid="channel-draft"]').exists()).toBe(false)
    clearAllDrafts()
  })
})

describe('server menu', () => {
  it('offers mark all read to everyone', async () => {
    usePermissionsStore()
    const menu = mount(SidebarServerMenu, { attachTo: document.body })
    await menu.find('[aria-controls="server-menu"]').trigger('click')
    const item = menu.findAll('#server-menu [role="menuitem"]').find(i => i.text().includes('gelesen') || i.text().toLowerCase().includes('read'))
    expect(item).toBeDefined()
    await item!.trigger('click')
    expect(menu.emitted('mark-all-read')).toHaveLength(1)
    menu.unmount()
  })
})
