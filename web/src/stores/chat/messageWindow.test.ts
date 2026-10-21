import { describe, it, expect, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../auth'
import { useVoiceStore } from '../voice'
import { createChatState, type Cx } from './state'
import {
  setWindow,
  resetWindow,
  countReply,
  bumpReplyCount,
  applyToMessage,
  loadedLists,
  onOriginalEdited,
  onOriginalDeleted,
  removeFromWindow,
  insertMessage
} from './messageWindow'
import { messageFixture, userFixture, channelFixture, fixtureId } from '../../test-fixtures.fixture'

describe('messageWindow', () => {
  let cx: Cx

  beforeEach(() => {
    setActivePinia(createPinia())
    const authStore = useAuthStore()
    const voiceStore = useVoiceStore()
    const cs = createChatState(() => {})
    cx = { cs, authStore, voiceStore }
    authStore.user = userFixture({ id: fixtureId(1) })
    cs.activeChannel.value = channelFixture({ id: fixtureId(2) })
  })

  describe('window lifecycle', () => {
    it('sets window state and flushes pending live messages when reaching newest', () => {
      const msg1 = messageFixture({ id: fixtureId(10), channel_id: fixtureId(2), content: 'one' })
      const liveMsg = messageFixture({ id: fixtureId(11), channel_id: fixtureId(2), content: 'live' })

      cx.cs.pendingLive.push(liveMsg)
      cx.cs.missedLiveCount.value = 1

      setWindow(cx, { messages: [msg1], hasMoreBefore: true, hasMoreAfter: false })

      expect(cx.cs.messages.value).toHaveLength(2)
      expect(cx.cs.messages.value[0]?.id).toBe(msg1.id)
      expect(cx.cs.messages.value[1]?.id).toBe(liveMsg.id)
      expect(cx.cs.hasMoreBefore.value).toBe(true)
      expect(cx.cs.hasMoreAfter.value).toBe(false)
      expect(cx.cs.missedLiveCount.value).toBe(0)
    })

    it('resets window loading flags and increments generation', () => {
      cx.cs.isLoadingBefore.value = true
      cx.cs.isLoadingAfter.value = true
      cx.cs.isLoadingWindow.value = true
      cx.cs.missedLiveCount.value = 5

      const prevGen = cx.cs.windowGen
      resetWindow(cx)

      expect(cx.cs.windowGen).toBe(prevGen + 1)
      expect(cx.cs.isLoadingBefore.value).toBe(false)
      expect(cx.cs.isLoadingAfter.value).toBe(false)
      expect(cx.cs.isLoadingWindow.value).toBe(false)
      expect(cx.cs.missedLiveCount.value).toBe(0)
    })
  })

  describe('message insertion and buffering', () => {
    it('appends live message directly when at the newest end', () => {
      const msg = messageFixture({ id: fixtureId(20), channel_id: fixtureId(2), content: 'direct' })
      cx.cs.hasMoreAfter.value = false
      cx.cs.isLoadingWindow.value = false

      insertMessage(cx, msg)

      expect(cx.cs.messages.value).toContainEqual(msg)
      expect(cx.cs.liveAppendSeq.value).toBe(1)
    })

    it('buffers live message when window is scrolled into the past', () => {
      const otherUserMsg = messageFixture({ id: fixtureId(21), channel_id: fixtureId(2), user_id: fixtureId(99), content: 'older' })
      cx.cs.hasMoreAfter.value = true

      insertMessage(cx, otherUserMsg)

      expect(cx.cs.messages.value).toHaveLength(0)
      expect(cx.cs.pendingLive).toContainEqual(otherUserMsg)
      expect(cx.cs.missedLiveCount.value).toBe(1)
    })

    it('inserts thread reply into active thread', () => {
      const root = messageFixture({ id: fixtureId(30), channel_id: fixtureId(2) })
      const reply = messageFixture({ id: fixtureId(31), parent_id: root.id, channel_id: fixtureId(2), content: 'reply' })
      cx.cs.activeThread.value = root

      insertMessage(cx, reply)

      expect(cx.cs.threadReplies.value).toContainEqual(reply)
    })
  })

  describe('replies and counting', () => {
    it('counts reply once and bumps reply count on root message and active thread', () => {
      const root = messageFixture({ id: fixtureId(40), channel_id: fixtureId(2), reply_count: 0 })
      const reply = messageFixture({ id: fixtureId(41), parent_id: root.id, channel_id: fixtureId(2) })
      cx.cs.messages.value = [root]
      cx.cs.activeThread.value = { ...root }

      countReply(cx, reply)
      expect(root.reply_count).toBe(1)
      expect(cx.cs.activeThread.value.reply_count).toBe(1)

      // Second count call is a no-op due to countedReplies
      countReply(cx, reply)
      expect(root.reply_count).toBe(1)
    })

    it('bumps reply count with floor of zero', () => {
      const root = messageFixture({ id: fixtureId(45), channel_id: fixtureId(2), reply_count: 1 })
      cx.cs.messages.value = [root]

      bumpReplyCount(cx, root.id, -5)
      expect(root.reply_count).toBe(0)
    })
  })

  describe('removal and mutations', () => {
    it('removes message from window and pending live queue', () => {
      const msg = messageFixture({ id: fixtureId(50), channel_id: fixtureId(2) })
      const pending = messageFixture({ id: fixtureId(51), channel_id: fixtureId(2) })
      cx.cs.messages.value = [msg]
      cx.cs.pendingLive = [pending]

      removeFromWindow(cx, msg.id)
      removeFromWindow(cx, pending.id)

      expect(cx.cs.messages.value).toHaveLength(0)
      expect(cx.cs.pendingLive).toHaveLength(0)
    })

    it('applies mutations to message in window, replies, or active thread', () => {
      const root = messageFixture({ id: fixtureId(60), content: 'original root' })
      const reply = messageFixture({ id: fixtureId(61), parent_id: root.id, content: 'original reply' })
      cx.cs.messages.value = [root]
      cx.cs.threadReplies.value = [reply]
      cx.cs.activeThread.value = { ...root }

      applyToMessage(cx, root.id, m => { m.content = 'mutated root' })
      applyToMessage(cx, reply.id, m => { m.content = 'mutated reply' })

      expect(root.content).toBe('mutated root')
      expect(cx.cs.activeThread.value.content).toBe('mutated root')
      expect(reply.content).toBe('mutated reply')
    })

    it('returns loaded lists of messages, replies, and active thread', () => {
      const root = messageFixture({ id: fixtureId(70) })
      const reply = messageFixture({ id: fixtureId(71), parent_id: root.id })
      cx.cs.messages.value = [root]
      cx.cs.threadReplies.value = [reply]
      cx.cs.activeThread.value = root

      const lists = loadedLists(cx)
      expect(lists).toEqual([[root], [reply], [root]])
    })

    it('updates replied-to previews when original message is edited or deleted', () => {
      const original = messageFixture({ id: fixtureId(80), content: 'before' })
      const reply = messageFixture({
        id: fixtureId(81),
        reply_to_id: original.id,
        reply_to: { id: original.id, content: 'before', user_id: fixtureId(1), username: 'user', deleted: false }
      })
      cx.cs.messages.value = [reply]

      onOriginalEdited(cx, { ...original, content: 'after' })
      expect(reply.reply_to?.content).toBe('after')

      onOriginalDeleted(cx, original.id)
      expect(reply.reply_to?.deleted).toBe(true)
    })
  })
})
