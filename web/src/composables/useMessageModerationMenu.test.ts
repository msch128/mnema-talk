import { required } from '../store-test-support.fixture'
import { useMessageActions } from './useMessageActions'
import type { ContextMenuItem } from '../components/menuTypes'
import { channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageMenu } from './useMessageMenu'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { pendingMessageModeration } from '../lib/messageModeration'
import { pendingEditHistory, closeEditHistory } from '../lib/editHistory'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale } from '../i18n'

const author = userFixture({ id: 'u2', username: 'talker', display_name: 'Talker', role: 'user' })
const msg = messageFixture({ id: 'm1', content: 'rude words', user_id: 'u2', username: 'talker', display_name: 'Talker', channel_id: 'ch1' })

function makeActions({ own = false, canDelete = true } = {}) {
  return {
    ...useMessageActions(),
    pickerId: ref<string | null>(null),
    isOwn: vi.fn(() => own),
    canDelete: vi.fn(() => canDelete),
    startEdit: vi.fn(),
    deleteMessage: vi.fn(async () => {}),
    closePicker: vi.fn()
  }
}

let chat: ReturnType<typeof useChatStore>
let permissions: ReturnType<typeof usePermissionsStore>
const labels = (items: ContextMenuItem[]) => items.filter(i => i.type !== 'separator').map(i => i.label)
const item = (items: ContextMenuItem[], label: string) => required(items.find(i => i.label === label))

function moderate(rank = 5) {
  permissions.info = {
    owner: false, permissions: 0, role_ids: [], top_position: rank, max_upload_mb: 50, catalog: [],
    channels: [{ channel_id: 'ch1', permissions: PERMISSION_BITS.manage_messages | PERMISSION_BITS.view_channel }]
  }
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  pendingMessageModeration.value = null
  chat = useChatStore()
  permissions = usePermissionsStore()
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
  chat.members = [author]
})

describe('message menu for moderators', () => {
  it('offers nothing extra without Manage messages', () => {
    permissions.info = { owner: false, permissions: 0, role_ids: [], top_position: 5, max_upload_mb: 50, catalog: [], channels: [{ channel_id: 'ch1', permissions: PERMISSION_BITS.view_channel }] }
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    const list = labels(menu.itemsFor(msg))
    for (const label of ['Pin message', 'Delete with reason…', 'Delete latest messages of this member…']) expect(list).not.toContain(label)
  })

  it('pins and unpins, reporting a failure', async () => {
    moderate()
    const pin = vi.spyOn(chat, 'setPinned').mockResolvedValue(msg)
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    await required(item(menu.itemsFor(msg), 'Pin message').action)()
    expect(pin).toHaveBeenCalledWith('ch1', 'm1', true)
    await required(item(menu.itemsFor({ ...msg, is_pinned: true }), 'Unpin message').action)()
    expect(pin).toHaveBeenLastCalledWith('ch1', 'm1', false)
    pin.mockRejectedValueOnce(new Error('no way'))
    await required(item(menu.itemsFor(msg), 'Pin message').action)()
    expect(useToastStore().toasts.map(t => t.text)).toContain('no way')
    pin.mockRejectedValueOnce('plain')
    await required(item(menu.itemsFor(msg), 'Pin message').action)()
    expect(useToastStore().toasts.map(t => t.text)).toContain('The action failed.')
  })

  it('opens the dialogs for deleting with a reason and for clearing a member', () => {
    moderate()
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    const items = menu.itemsFor(msg)
    required(item(items, 'Delete with reason…').action)()
    expect(pendingMessageModeration.value?.request).toEqual({ kind: 'delete', channelId: 'ch1', messageId: 'm1', excerpt: 'rude words' })
    required(item(items, 'Delete latest messages of this member…').action)()
    expect(pendingMessageModeration.value?.request).toEqual({ kind: 'bulk', channelId: 'ch1', userId: 'u2', name: 'Talker' })
    required(item(menu.itemsFor({ ...msg, display_name: '' }), 'Delete latest messages of this member…').action)()
    expect(pendingMessageModeration.value?.request).toMatchObject({ name: 'talker' })
  })

  it('does not offer reasons or clearing for own messages, bots, the Owner or equal rank', () => {
    moderate()
    const own = labels(useMessageMenu({ actions: makeActions({ own: true }), reply: vi.fn() }).itemsFor(msg))
    expect(own).not.toContain('Delete with reason…')
    expect(own).not.toContain('Delete latest messages of this member…')
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    expect(labels(menu.itemsFor({ ...msg, is_bot: true }))).not.toContain('Delete latest messages of this member…')
    chat.members = [{ ...author, role: 'admin' }]
    expect(labels(menu.itemsFor(msg))).not.toContain('Delete latest messages of this member…')
    chat.members = []
    expect(labels(menu.itemsFor(msg))).not.toContain('Delete latest messages of this member…')
    chat.members = [author]
    permissions.setMemberRoles([{ id: 'u2', role_ids: ['r9'] }])
    permissions.roles = [{ id: 'r9', name: 'Mod', color: 0, position: 5, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }]
    expect(labels(menu.itemsFor(msg))).not.toContain('Delete latest messages of this member…')
    // Plain delete stays when allowed, and goes when not.
    expect(labels(menu.itemsFor(msg))).toContain('Delete with reason…')
    expect(labels(useMessageMenu({ actions: makeActions({ canDelete: false }), reply: vi.fn() }).itemsFor(msg))).not.toContain('Delete with reason…')
  })
})

describe('message menu: edit history', () => {
  const edited = { ...msg, is_edited: true, updated_at: '2026-01-02T10:00:00Z' }

  it('is offered to moderators for edited messages only', () => {
    moderate()
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    expect(labels(menu.itemsFor(edited))).toContain('Edit history')
    expect(labels(menu.itemsFor(msg))).not.toContain('Edit history')
  })

  it('is hidden from members without Manage messages, even for their own edits', () => {
    permissions.info = { owner: false, permissions: 0, role_ids: [], top_position: 5, max_upload_mb: 50, catalog: [], channels: [{ channel_id: 'ch1', permissions: PERMISSION_BITS.view_channel }] }
    const menu = useMessageMenu({ actions: makeActions({ own: true }), reply: vi.fn() })
    expect(labels(menu.itemsFor(edited))).not.toContain('Edit history')
  })

  it('opens the dialog for that message', () => {
    moderate()
    closeEditHistory()
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    required(item(menu.itemsFor(edited), 'Edit history').action)()
    expect(pendingEditHistory.value).toEqual({ channelId: 'ch1', messageId: 'm1', current: 'rude words', editedAt: '2026-01-02T10:00:00Z' })
    closeEditHistory()
  })
})
