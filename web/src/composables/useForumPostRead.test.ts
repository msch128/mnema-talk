import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useForumPostRead } from './useForumPostRead'
import * as forumApi from '../lib/forumApi'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'

vi.mock('../lib/forumApi', () => ({ markForumPostRead: vi.fn() }))

let read!: ReturnType<typeof useForumPostRead>
let chat: ReturnType<typeof useChatStore>
const reply = (id: string, user: string, at: string) => messageFixture({ id, user_id: user, created_at: at })

let mounted: ReturnType<typeof mount> | undefined

function host() {
  mounted = mount(defineComponent({ setup() { read = useForumPostRead(); return () => null } }))
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.useFakeTimers()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  useAuthStore().user = userFixture({ id: 'me' })
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', type: 'forum' })
  vi.mocked(forumApi.markForumPostRead).mockResolvedValue('2026-01-01T10:00:00Z')
})
afterEach(() => {
  mounted?.unmount()
  vi.useRealTimers()
})

describe('useForumPostRead', () => {
  it('marks an opened post read and lists the replies of others after the old marker', async () => {
    chat.activeThread = messageFixture({ id: 'p1', channel_id: 'ch1', title: 'T' })
    chat.threadReplies = [reply('old', 'o', '2026-01-01T09:00:00Z'), reply('own', 'me', '2026-01-01T11:00:00Z'), reply('n1', 'o', '2026-01-01T12:00:00Z')]
    host()
    await flushPromises()
    expect(forumApi.markForumPostRead).toHaveBeenCalledWith('ch1', 'p1')
    expect(read.unreadReplies.value.map(r => r.id)).toEqual(['n1'])
    expect(read.firstUnreadId.value).toBe('n1')
    read.dismiss()
    expect(read.unreadReplies.value).toEqual([])
  })

  it('ignores a thread that is no forum post', async () => {
    chat.activeThread = messageFixture({ id: 'p1', channel_id: 'ch1' })
    host()
    await flushPromises()
    expect(forumApi.markForumPostRead).not.toHaveBeenCalled()
    expect(read.firstUnreadId.value).toBeNull()
  })

  it('marks again, without a new jump target, after replies arrive while it is open', async () => {
    chat.activeThread = messageFixture({ id: 'p1', channel_id: 'ch1', title: 'T' })
    host()
    await flushPromises()
    vi.mocked(forumApi.markForumPostRead).mockClear()
    chat.threadReplies = [reply('n1', 'o', '2026-01-01T12:00:00Z')]
    await vi.advanceTimersByTimeAsync(500)
    expect(forumApi.markForumPostRead).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(600)
    expect(forumApi.markForumPostRead).toHaveBeenCalledTimes(1)
    // The marker of the first opening still decides what is "new".
    expect(read.firstUnreadId.value).toBe('n1')
  })

  it('starts over for another post and survives a failing request', async () => {
    chat.activeThread = messageFixture({ id: 'p1', channel_id: 'ch1', title: 'T' })
    host()
    await flushPromises()
    vi.mocked(forumApi.markForumPostRead).mockRejectedValueOnce(new Error('offline'))
    chat.threadReplies = []
    chat.activeThread = messageFixture({ id: 'p2', channel_id: 'ch1', title: 'U' })
    chat.threadReplies = [reply('n1', 'o', '2026-01-01T12:00:00Z')]
    await flushPromises()
    expect(forumApi.markForumPostRead).toHaveBeenLastCalledWith('ch1', 'p2')
    expect(read.unreadReplies.value).toEqual([])
  })
})
