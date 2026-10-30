import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useForumPostActions } from './useForumPostActions'
import * as forumApi from '../lib/forumApi'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../lib/confirm'
import { channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Message } from '../types/domain'

vi.mock('../lib/forumApi', () => ({ updateForumPost: vi.fn() }))

let actions!: ReturnType<typeof useForumPostActions>
let chat: ReturnType<typeof useChatStore>
let mounted: ReturnType<typeof mount> | undefined

function setup({ moderator = false, author = true, extra = {} as Partial<Message> } = {}) {
  useAuthStore().user = userFixture({ id: 'me' })
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', type: 'forum' })
  vi.spyOn(usePermissionsStore(), 'canIn').mockImplementation((_c, perm) => perm === 'manage_messages' ? moderator : true)
  chat.activeThread = messageFixture({ id: 'p1', channel_id: 'ch1', user_id: author ? 'me' : 'other', title: 'Post', ...extra })
  mounted = mount(defineComponent({ setup() { actions = useForumPostActions(); return () => null } }))
}

const toastText = () => useToastStore().toasts.map(x => x.text).join('|')

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.mocked(forumApi.updateForumPost).mockImplementation(async (_c, _p, patch) => messageFixture({ id: 'p1', ...(patch as Partial<Message>) }))
})
afterEach(() => {
  pendingConfirm.value = null
  mounted?.unmount()
})

describe('useForumPostActions permissions', () => {
  it('lets the author edit and delete an open post, but not edit a locked one', async () => {
    setup()
    expect(actions.canEdit.value).toBe(true)
    expect(actions.canDelete.value).toBe(true)
    chat.activeThread = messageFixture({ id: 'p1', channel_id: 'ch1', user_id: 'me', title: 'Post', is_locked: true })
    expect(actions.canEdit.value).toBe(false)
    expect(actions.canDelete.value).toBe(true)
  })

  it('lets a moderator do everything, also on a locked post of someone else', () => {
    setup({ moderator: true, author: false, extra: { is_locked: true } })
    expect(actions.canEdit.value).toBe(true)
    expect(actions.canDelete.value).toBe(true)
    expect(actions.canModerate.value).toBe(true)
  })

  it('gives a stranger nothing and falls back to the active channel without a post', () => {
    setup({ author: false })
    expect(actions.canEdit.value).toBe(false)
    expect(actions.canDelete.value).toBe(false)
    chat.activeThread = null
    expect(actions.channelId.value).toBe('ch1')
    expect(actions.canEdit.value).toBe(false)
  })
})

describe('useForumPostActions changes', () => {
  it('sends the change and applies the answer to the open post', async () => {
    setup()
    await expect(actions.rename('New')).resolves.toBe(true)
    expect(forumApi.updateForumPost).toHaveBeenCalledWith('ch1', 'p1', { title: 'New' })
    expect(chat.activeThread?.title).toBe('New')
    await actions.retag(['a'])
    await actions.setSolved(true)
    await actions.setLocked(true)
    expect(vi.mocked(forumApi.updateForumPost).mock.calls.map(c => c[2])).toEqual([{ title: 'New' }, { tag_ids: ['a'] }, { is_resolved: true }, { is_locked: true }])
  })

  it('reports a refusal as a toast and resolves to false', async () => {
    setup()
    vi.mocked(forumApi.updateForumPost).mockRejectedValue(new Error('the post is locked'))
    await expect(actions.retag(['a'])).resolves.toBe(false)
    expect(toastText()).toContain('the post is locked')
  })

  it('does nothing without an open post', async () => {
    setup()
    chat.activeThread = null
    await expect(actions.rename('x')).resolves.toBe(false)
    await actions.setPinned(true)
    await actions.remove()
    expect(forumApi.updateForumPost).not.toHaveBeenCalled()
  })

  it('does not paint the answer on another post that was opened meanwhile', async () => {
    setup()
    let release!: (m: Message) => void
    vi.mocked(forumApi.updateForumPost).mockImplementationOnce(() => new Promise(r => { release = r }))
    const pending = actions.rename('Late')
    chat.activeThread = messageFixture({ id: 'p2', channel_id: 'ch1', title: 'Other' })
    release(messageFixture({ id: 'p1', title: 'Late' }))
    await pending
    expect(chat.activeThread?.title).toBe('Other')
  })
})

describe('useForumPostActions pin and delete', () => {
  it('pins through the store and toasts when that fails', async () => {
    setup({ moderator: true })
    const pin = vi.spyOn(chat, 'setPinned').mockResolvedValueOnce(undefined as never)
    await actions.setPinned(true)
    expect(pin).toHaveBeenCalledWith('ch1', 'p1', true)
    pin.mockRejectedValueOnce(new Error('too many pins'))
    await actions.setPinned(false)
    expect(toastText()).toContain('too many pins')
  })

  it('asks before deleting, naming the replies, and deletes after a yes', async () => {
    setup({ extra: { reply_count: 1 } })
    const del = vi.spyOn(chat, 'deleteMessage').mockResolvedValue(undefined as never)
    const run = actions.remove()
    await flushPromises()
    expect(pendingConfirm.value?.body).toContain('reply')
    expect(pendingConfirm.value?.excerpt).toBe('Post')
    pendingConfirm.value?.resolve(true)
    await run
    expect(del).toHaveBeenCalledWith('ch1', 'p1')
  })

  it('uses the plain text and the message excerpt for a post without replies or title, and keeps it on no', async () => {
    setup({ extra: { reply_count: 0, title: '', content: 'First words' } })
    const del = vi.spyOn(chat, 'deleteMessage').mockResolvedValue(undefined as never)
    const run = actions.remove()
    await flushPromises()
    expect(pendingConfirm.value?.body).toBe('The post is removed for everyone. This can’t be undone.')
    expect(pendingConfirm.value?.excerpt).toBe('First words')
    pendingConfirm.value?.resolve(false)
    await run
    expect(del).not.toHaveBeenCalled()
  })

  it('toasts when the delete fails', async () => {
    setup()
    vi.spyOn(chat, 'deleteMessage').mockRejectedValue(new Error('gone wrong'))
    const run = actions.remove()
    await flushPromises()
    pendingConfirm.value?.resolve(true)
    await run
    expect(toastText()).toContain('gone wrong')
  })
})
