import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../auth'
import { useVoiceStore } from '../voice'
import { createChatState, type Cx } from './state'
import {
  activityOfUser,
  applyActivity,
  presenceOf,
  countMessageOf,
  updateUserEverywhere,
  closeUserProfile,
  patchOpenProfile,
  openUserProfile,
  insertMention
} from './members'
import { userFixture, messageFixture, fixtureId } from '../../test-fixtures.fixture'
import type { Member } from '../../types/domain'

describe('members', () => {
  let cx: Cx

  beforeEach(() => {
    setActivePinia(createPinia())
    const authStore = useAuthStore()
    const voiceStore = useVoiceStore()
    const cs = createChatState(() => {})
    cx = { cs, authStore, voiceStore }
  })

  describe('opening a profile', () => {
    it('falls back to the candidate username, not the known member, for the display name', async () => {
      const id = fixtureId(7)
      const fetchMock = vi.fn(async () => new Response(JSON.stringify(userFixture({ id, username: 'known' })), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      vi.stubGlobal('fetch', fetchMock)
      try {
        cx.cs.members.value = [userFixture({ id, username: 'known', display_name: '' })]
        const opening = openUserProfile(cx, { id, username: 'given' })
        expect(cx.cs.selectedUserProfile.value).toMatchObject({ username: 'given', display_name: 'given' })
        await opening
        cx.cs.selectedUserProfile.value = null
        const bare = openUserProfile(cx, { id })
        expect(cx.cs.selectedUserProfile.value).toMatchObject({ username: 'known', display_name: '' })
        await bare
        expect(fetchMock).toHaveBeenCalledTimes(2)
      } finally {
        vi.unstubAllGlobals()
      }
    })
  })

  describe('presence and activity', () => {
    it('returns presence of user or offline for null/unknown', () => {
      const u1 = fixtureId(1)
      expect(presenceOf(cx, null)).toBe('offline')
      expect(presenceOf(cx, u1)).toBe('offline')

      cx.cs.presenceById.value = { [u1]: 'online' }
      expect(presenceOf(cx, u1)).toBe('online')
    })

    it('manages bot activity with status and offline transitions', () => {
      const botId = fixtureId(2)
      expect(activityOfUser(cx, null)).toBeNull()
      expect(activityOfUser(cx, botId)).toBeNull()

      applyActivity(cx, { user_id: botId, status: 'online', activity: { type: 0, name: 'Playing chess' } })
      expect(activityOfUser(cx, botId)).toEqual({ type: 0, name: 'Playing chess' })

      applyActivity(cx, { user_id: botId, status: 'offline' })
      expect(activityOfUser(cx, botId)).toBeNull()
    })
  })

  describe('member statistics and profile', () => {
    it('increments member message count', () => {
      const member: Member = { ...userFixture({ id: fixtureId(3) }), message_count: 5 }
      cx.cs.members.value = [member]

      countMessageOf(cx, member.id)
      expect(member.message_count).toBe(6)

      // Unknown user does not throw
      countMessageOf(cx, fixtureId(999))
    })

    it('sets and clears pending mention', () => {
      expect(cx.cs.pendingMention.value).toBe('')

      insertMention(cx, 'alice')
      expect(cx.cs.pendingMention.value).toBe('alice')

      insertMention(cx, '')
      expect(cx.cs.pendingMention.value).toBe('alice')
    })

    it('closes user profile preview', () => {
      cx.cs.selectedUserProfile.value = {
        id: fixtureId(4),
        username: 'bob',
        display_name: 'Bob',
        avatar_url: '',
        role: 'user'
      }

      closeUserProfile(cx)
      expect(cx.cs.selectedUserProfile.value).toBeNull()
    })
  })

  describe('patching the open profile', () => {
    it('ignores a save that finishes after another profile was opened', () => {
      const mine = fixtureId(20)
      const other = fixtureId(21)
      cx.cs.selectedUserProfile.value = { id: other, username: 'bob', display_name: 'Bob', role: 'user' }
      patchOpenProfile(cx, mine, { id: mine, username: 'ada', display_name: 'Ada', bio: 'mine' })
      expect(cx.cs.selectedUserProfile.value).toMatchObject({ id: other, username: 'bob', display_name: 'Bob' })
      patchOpenProfile(cx, mine, { bio: 'x' })
      closeUserProfile(cx)
      patchOpenProfile(cx, mine, { bio: 'x' })
      expect(cx.cs.selectedUserProfile.value).toBeNull()
    })

    it('merges into the profile it was started on and drops cleared fields', () => {
      const id = fixtureId(22)
      cx.cs.selectedUserProfile.value = { id, username: 'ada', display_name: 'Ada', role: 'user', avatar_url: '/old', bio: 'a' }
      patchOpenProfile(cx, id, { bio: 'b' }, ['avatar_url'])
      expect(cx.cs.selectedUserProfile.value).toEqual({ id, username: 'ada', display_name: 'Ada', role: 'user', bio: 'b' })
    })
  })

  describe('user updates everywhere', () => {
    it('applies disabled update by setting offline and removing from members list', () => {
      const uId = fixtureId(10)
      const member: Member = { ...userFixture({ id: uId }) }
      cx.cs.members.value = [member]
      cx.cs.presenceById.value = { [uId]: 'online' }

      updateUserEverywhere(cx, { id: uId, disabled: true })

      expect(cx.cs.members.value.find(m => m.id === uId)).toBeUndefined()
      expect(presenceOf(cx, uId)).toBe('offline')
      expect(cx.cs.disabledMembers.get(uId)).toBeDefined()
    })

    it('applies profile update to member, current user, and loaded message authors', () => {
      const uId = fixtureId(11)
      const member: Member = { ...userFixture({ id: uId, display_name: 'Old Name' }) }
      cx.cs.members.value = [member]
      cx.authStore.user = userFixture({ id: uId, display_name: 'Old Name' })

      const msg = messageFixture({ id: fixtureId(20), user_id: uId, display_name: 'Old Name' })
      cx.cs.messages.value = [msg]

      updateUserEverywhere(cx, userFixture({ id: uId, display_name: 'New Name', username: 'newuser' }))

      expect(cx.cs.members.value[0]?.display_name).toBe('New Name')
      expect(cx.authStore.user?.display_name).toBe('New Name')
      expect(msg.display_name).toBe('New Name')
    })
  })
})
