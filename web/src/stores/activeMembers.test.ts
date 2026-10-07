import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { useChatStore } from './chat'
import { fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'
import type { User } from '../types/domain'

function deferred<T>() {
  let resolve: (value: T) => void = () => { throw new Error('Missing deferred resolver') }
  let reject: (reason: Error) => void = () => { throw new Error('Missing deferred rejector') }
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const me = userFixture({ id: fixtureId(1), username: 'current-member' })
const member = userFixture({ id: fixtureId(7), username: 'anna', display_name: 'Anna', avatar_url: '/media/synthetic-avatar' })
const other = userFixture({ id: fixtureId(8), username: 'ben', display_name: 'Ben' })
function pendingMembers() {
  const pending = deferred<Response>()
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(() => pending.promise)
  vi.stubGlobal('fetch', fetch)
  return { fetch, answer: (users: User[]) => pending.resolve(new Response(JSON.stringify(users), { status: 200 })), fail: pending.reject }
}
function setup() {
  useAuthStore().user = { ...me }
  const chat = useChatStore()
  chat.members = [{ ...member }, { ...other }]
  return chat
}
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
beforeEach(() => {
  setActivePinia(createPinia()); vi.useFakeTimers()
  vi.spyOn(window, 'addEventListener'); vi.spyOn(document, 'addEventListener')
})
afterEach(() => {
  useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers()
  for (const [type, listener, options] of vi.mocked(window.addEventListener).mock.calls) window.removeEventListener(type, listener, options)
  for (const [type, listener, options] of vi.mocked(document.addEventListener).mock.calls) document.removeEventListener(type, listener, options)
  vi.restoreAllMocks(); vi.unstubAllGlobals()
})

describe('active community members', () => {
  it('removes a disabled member without deleting history or blanking author/profile fields', () => {
    const chat = setup()
    const root = messageFixture({ user_id: member.id, display_name: member.display_name, avatar_url: required(member.avatar_url) })
    const reply = messageFixture({ id: fixtureId(9), user_id: other.id, reply_to: { id: root.id, user_id: member.id, display_name: member.display_name, avatar_url: required(member.avatar_url), content: root.content, deleted: false } })
    chat.messages = [root, reply]; chat.activeThread = { ...root }; chat.threadReplies = [{ ...reply }]; chat.selectedUserProfile = { ...member }
    chat.handleWSEvent({ type: 'presence_snapshot', payload: { [member.id]: 'online', [other.id]: 'away' } })
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    expect(chat.members).toEqual([other]); expect(chat.onlineMembers).toEqual([other]); expect(chat.offlineMembers).toEqual([])
    expect(chat.presenceOf(member.id)).toBe('offline'); expect(chat.onlineUserIds.has(member.id)).toBe(false)
    expect(chat.messages).toEqual([root, reply]); expect(chat.activeThread).toEqual(root); expect(chat.threadReplies).toEqual([reply])
    expect(chat.selectedUserProfile).toMatchObject(member)
  })

  it('removes offline members and ignores late presence events for disabled accounts', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: member.id, status: 'online' } })
    expect(chat.onlineUserIds.has(member.id)).toBe(false)
    chat.handleWSEvent({ type: 'presence_snapshot', payload: [member.id, other.id] })
    expect(chat.onlineUserIds).toEqual(new Set([other.id])); expect(chat.offlineMembers).toEqual([])
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: other.id, status: 'focus' } })
    expect(chat.presenceOf(other.id)).toBe('focus')
  })

  it('prevents a request started before disabling from resurrecting that member', async () => {
    const chat = setup(); const request = pendingMembers(); const loading = chat.fetchMembers()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    request.answer([member, other]); await loading
    expect(chat.members).toEqual([other]); expect(request.fetch).toHaveBeenCalledWith('/api/members', expect.any(Object))
  })

  it('accepts the newest roster when two requests resolve in reverse order', async () => {
    const chat = setup(); const older = pendingMembers(); const oldLoading = chat.fetchMembers()
    const newer = pendingMembers(); const newLoading = chat.fetchMembers()
    newer.answer([other]); await newLoading; older.answer([member, other]); await oldLoading
    expect(chat.members).toEqual([other])
  })

  it('ignores an older request even when it completes before the newest request', async () => {
    const chat = setup(); const older = pendingMembers(); const oldLoading = chat.fetchMembers()
    const newer = pendingMembers(); const newLoading = chat.fetchMembers()
    older.answer([]); await oldLoading; expect(chat.members).toEqual([member, other])
    newer.answer([other]); await newLoading; expect(chat.members).toEqual([other])
  })

  it('re-enables through a refetch, without inserting a partial user object', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    const request = pendingMembers()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: false } })
    expect(request.fetch).toHaveBeenCalledTimes(1); expect(chat.members).toEqual([other])
    request.answer([member, other]); await flush(); expect(chat.members).toEqual([member, other])
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: member.id, status: 'online' } })
    expect(chat.onlineMembers).toEqual([member])
  })

  it('restores a re-enabled member from a fresh roster when the enable event was missed offline', async () => {
    const chat = setup(); const oldRequest = pendingMembers(); const oldLoading = chat.fetchMembers()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    oldRequest.answer([member, other]); await oldLoading; expect(chat.members).toEqual([other])
    const freshRequest = pendingMembers(); const reconnectLoading = chat.fetchMembers()
    freshRequest.answer([member, other]); await reconnectLoading; expect(chat.members).toEqual([member, other])
    chat.handleWSEvent({ type: 'presence_snapshot', payload: [member.id] }); expect(chat.onlineMembers).toEqual([member])
  })

  it('keeps a new disable event ahead of an overlapping re-enable request and late profile changes', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    const reenable = pendingMembers()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: false } })
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    chat.handleWSEvent({ type: 'user_update', payload: { ...member, display_name: 'Late profile' } })
    reenable.answer([member, other]); await flush()
    expect(chat.members).toEqual([other]); expect(chat.presenceOf(member.id)).toBe('offline')
  })

  it('keeps profile fields updated during a fetch, but lets subsequent authoritative rosters replace them', async () => {
    const chat = setup(); const request = pendingMembers(); const loading = chat.fetchMembers()
    const updated = { ...member, display_name: 'Updated name', avatar_url: '/media/updated-avatar' }
    chat.handleWSEvent({ type: 'user_update', payload: updated }); request.answer([member, other]); await loading
    expect(chat.members).toEqual([updated, other])
    const later = pendingMembers(); const refreshing = chat.fetchMembers()
    later.answer([{ ...updated, display_name: 'Fresh server name' }, other]); await refreshing
    expect(required(chat.members[0]).display_name).toBe('Fresh server name')
  })

  it('does not treat unknown profile updates as membership; registration refetches new members', async () => {
    const chat = setup(); const newcomer = userFixture({ id: fixtureId(10), username: 'newcomer' })
    chat.handleWSEvent({ type: 'user_update', payload: newcomer }); expect(chat.members).toEqual([member, other])
    const request = pendingMembers(); chat.handleWSEvent({ type: 'member_joined', payload: newcomer })
    request.answer([member, other, newcomer]); await flush(); expect(chat.members).toEqual([member, other, newcomer])
  })

  it('preserves author edits for disabled users without re-adding them as active members', () => {
    const chat = setup(); chat.messages = [messageFixture({ user_id: member.id, display_name: 'Anna' })]; chat.selectedUserProfile = { ...member }
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    chat.handleWSEvent({ type: 'user_update', payload: { ...member, display_name: 'Historical author' } })
    expect(chat.members).toEqual([other]); expect(required(chat.messages[0]).display_name).toBe('Historical author')
    expect(chat.selectedUserProfile?.display_name).toBe('Historical author')
  })

  it('keeps the removal after a failed re-enable refetch and can retry with a fresh roster', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    const request = pendingMembers(); const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: false } })
    request.fail(new TypeError('synthetic offline')); await flush()
    expect(chat.members).toEqual([other]); expect(error).toHaveBeenCalled()
    const retry = pendingMembers(); const loading = chat.fetchMembers()
    retry.answer([member, other]); await loading; expect(chat.members).toEqual([member, other])
  })
})

describe('presence reconciliation after a missed re-enable', () => {
  it('restores the reconnect presence snapshot arriving before the active roster', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    const request = pendingMembers(); const loading = chat.fetchMembers()
    chat.handleWSEvent({ type: 'presence_snapshot', payload: { [member.id]: 'focus', [other.id]: 'away' } })
    expect(chat.presenceOf(member.id)).toBe('offline')
    request.answer([member, other]); await loading
    expect(chat.presenceOf(member.id)).toBe('focus'); expect(chat.onlineMembers).toEqual([member, other])
  })

  it('keeps the most recent offline event when a reconnect snapshot is superseded before the roster arrives', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    const request = pendingMembers(); const loading = chat.fetchMembers()
    chat.handleWSEvent({ type: 'presence_snapshot', payload: [member.id] })
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: member.id, status: 'offline' } })
    request.answer([member, other]); await loading
    expect(chat.presenceOf(member.id)).toBe('offline'); expect(chat.offlineMembers).toEqual([member, other])
  })

  it('does not restore a presence absent from the latest snapshot or from before a repeated disable', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: member.id, status: 'online' } })
    chat.handleWSEvent({ type: 'presence_snapshot', payload: [] })
    const request = pendingMembers(); const loading = chat.fetchMembers()
    request.answer([member, other]); await loading
    expect(chat.presenceOf(member.id)).toBe('offline')
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: member.id, status: 'online' } })
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    const retry = pendingMembers(); const refresh = chat.fetchMembers()
    retry.answer([member, other]); await refresh
    expect(chat.presenceOf(member.id)).toBe('offline')
  })
})

describe('discarded connection presence', () => {
  it('does not restore suppressed presence retained from a connection that was closed', async () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'user_update', payload: { id: member.id, disabled: true } })
    chat.handleWSEvent({ type: 'presence_snapshot', payload: [member.id] })
    chat.closeWebSocket()
    const request = pendingMembers(); const loading = chat.fetchMembers()
    request.answer([member, other]); await loading
    expect(chat.members).toEqual([member, other]); expect(chat.presenceOf(member.id)).toBe('offline')
  })
})
