import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { useToastStore } from './toast'
import { channelFixture, categoryFixture, messageFixture, userFixture, readStateFixture, fixtureId } from '../test-fixtures.fixture'
const request = vi.hoisted(() => vi.fn())
vi.mock('../lib/api', async original => ({ ...(await original<typeof import('../lib/api')>()), api: request }))
function deferred() {
  let resolve!: (value: unknown) => void
  const promise = new Promise<unknown>(r => { resolve = r })
  return { promise, resolve }
}
beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers(); request.mockReset() })
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.restoreAllMocks() })
function populated() {
  const chat = useChatStore(), channel = channelFixture(), message = messageFixture({ channel_id: channel.id }), user = userFixture()
  useAuthStore().user = user
  chat.categories = [categoryFixture({ channels: [channel] })]
  chat.uncategorized = [channel]; chat.activeChannel = channel
  chat.messages = [message]; chat.activeThread = message; chat.threadReplies = [message]
  chat.members = [user]; chat.presenceById = { [user.id]: 'online' }
  chat.selectedUserProfile = user; chat.pendingMention = user.username
  chat.readStates = { [channel.id]: readStateFixture({ channel_id: channel.id }) }
  chat.typingByChannel = { [channel.id]: [{ user_id: user.id, username: user.username, display_name: user.display_name }] }
  return { chat, channel, message, user }
}
it('clears instance A before failed instance B metadata and rejects late A responses', async () => {
  const { chat, channel, message, user } = populated()
  const channels = deferred(), members = deferred(), reads = deferred(), profile = deferred(), messages = deferred(), thread = deferred()
  request.mockImplementation((path: string) => {
    if (path === '/api/channels') return channels.promise
    if (path === '/api/members') return members.promise
    if (path === '/api/read-state') return reads.promise
    if (path === `/api/users/${user.id}`) return profile.promise
    if (path.endsWith('/thread')) return thread.promise
    return messages.promise
  })
  const pending = [chat.fetchChannels(), chat.fetchMembers(), chat.fetchReadState(), chat.openUserProfile(user), chat.selectChannel(channel), chat.openThread(message)]
  useToastStore().error('Instance A private detail', { action: { label: 'Old action', onClick: vi.fn() } })
  useToastStore().info('Instance A transient message')
  chat.resetCommunityState()
  expect(useToastStore().toasts).toEqual([])
  request.mockRejectedValue(new Error('Instance B metadata unavailable'))
  await Promise.all([chat.fetchChannels(), chat.fetchMembers()])
  channels.resolve({ categories: [categoryFixture({ channels: [channel] })], uncategorized: [channel] })
  members.resolve([user]); reads.resolve([readStateFixture({ channel_id: channel.id })])
  profile.resolve(user); messages.resolve([message]); thread.resolve({ root: message, replies: [message] })
  await Promise.all(pending)
  expect(chat.allChannels).toEqual([]); expect(chat.activeChannel).toBeNull()
  expect(chat.messages).toEqual([]); expect(chat.members).toEqual([])
  expect(chat.readStates).toEqual({}); expect(chat.presenceById).toEqual({})
  expect(chat.typingByChannel).toEqual({}); expect(chat.selectedUserProfile).toBeNull()
  expect(chat.activeThread).toBeNull(); expect(chat.threadReplies).toEqual([])
  expect(chat.pendingMention).toBe(''); expect(chat.isConnected).toBe(false)
  expect(request.mock.calls.some(([path]) => String(path).endsWith('/read'))).toBe(false)
})
it('does not replace a newly opened same-ID profile with a retired instance response', async () => {
  const { chat, user } = populated(); const old = deferred()
  request.mockReturnValueOnce(old.promise)
  const pending = chat.openUserProfile(user)
  chat.resetCommunityState()
  const current = { ...user, username: 'instance-b' }
  request.mockResolvedValue(current)
  await chat.openUserProfile(current)
  old.resolve(user); await pending
  expect(chat.selectedUserProfile?.username).toBe('instance-b')
})
it('cannot let a cached old jump consume a new same-ID pending thread', async () => {
  const { chat, channel, message } = populated()
  const old = chat.jumpToMessage(message.id)
  chat.resetCommunityState()
  const current = { ...message, content: 'instance-b' }
  chat.activeChannel = channel; chat.messages = [current]
  chat.goToMessage(messageFixture({ id: fixtureId(40), channel_id: channel.id, parent_id: message.id }))
  expect(await old).toBe(false)
  expect(chat.activeThread).toBeNull(); expect(request).not.toHaveBeenCalled()
  request.mockResolvedValue({ root: current, replies: [] })
  expect(await chat.jumpToMessage(message.id)).toBe(true)
  await flushPromises()
  expect(chat.activeThread?.content).toBe('instance-b')
  expect(request).toHaveBeenCalledOnce()
})
it.each(['send', 'upload', 'presence', 'status', 'create-channel', 'duplicate-channel', 'delete-channel', 'create-category', 'delete-category', 'update-channel', 'update-category', 'unread', 'edit', 'delete', 'reaction'] as const)('retires %s mutation continuations before instance B caches or requests', async operation => {
  const { chat, channel, message, user } = populated(); const old = deferred()
  request.mockReturnValue(old.promise)
  const actions = {
    send: () => chat.sendMessage('Old instance message'),
    upload: () => chat.uploadMedia(new File(['synthetic'], 'fixture.txt')),
    presence: () => chat.setMyPresence('away'), status: () => chat.setStatusText(user.id, 'old'),
    'create-channel': () => chat.createChannel({ name: 'old' }), 'duplicate-channel': () => chat.duplicateChannel(channel.id),
    'delete-channel': () => chat.deleteChannel(channel.id), 'create-category': () => chat.createCategory('old'),
    'delete-category': () => chat.deleteCategory('old'), 'update-channel': () => chat.updateChannel(channel.id, { name: 'old', topic: '' }),
    'update-category': () => chat.updateCategory('old', { name: 'old' }), unread: () => chat.markChannelUnread(channel.id, message.id),
    edit: () => chat.editMessage(channel.id, message.id, 'old'), delete: () => chat.deleteMessage(channel.id, message.id),
    reaction: () => chat.toggleReaction(message.id, '👍')
  }
  const pending = actions[operation]()
  const rejected = expect(pending).rejects.toThrow('Community request retired')
  chat.resetCommunityState()
  const currentUser = { ...user, username: 'instance-b', presence: 'dnd' as const }
  useAuthStore().user = currentUser; chat.members = [currentUser]
  const currentMessage = { ...message, content: 'instance-b' }; chat.messages = [currentMessage]
  old.resolve(operation === 'presence' || operation === 'status' ? user : operation === 'create-channel' || operation === 'duplicate-channel' ? channel : operation === 'reaction' ? { reactions: [] } : message)
  await rejected; await flushPromises()
  expect(chat.messages).toEqual([currentMessage]); expect(chat.members).toEqual([currentUser])
  expect(useAuthStore().user).toEqual(currentUser)
  expect(request).toHaveBeenCalledOnce()
})
