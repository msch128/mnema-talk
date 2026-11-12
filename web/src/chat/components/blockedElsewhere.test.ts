// A block folds the blocked member's messages: "You don't see messages from
// members you block" (settings, blocks.intro), "Their messages are collapsed in
// every list" (stores/blocks.ts), and doc/moderation.md "Blocking a member".
// The message list and the thread fold them (ChatAreaDm.test.ts). These are
// the other places that show a blocked member's words (finding H-L3-10 of the
// 2026-11 hunt); each goes through shared/components/BlockedFold.vue.
import type { ApiOptions } from '../../shared/lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import '../../shared/lib/customEmoji'
import ReplyPreview from './ReplyPreview.vue'
import SearchResultItem from './SearchResultItem.vue'
import ForwardedQuote from './ForwardedQuote.vue'
import PinnedMessages from './PinnedMessages.vue'
import SavedEntry from './SavedEntry.vue'
import LaterList from './LaterList.vue'
import ForumPostCard from '../../forum/components/ForumPostCard.vue'
import ForumPostHeader from '../../forum/components/ForumPostHeader.vue'
import EventComments from '../../events/components/EventComments.vue'
import BlockedMessageRow from './BlockedMessageRow.vue'
import { defineComponent, h } from 'vue'
import * as eventApi from '../../events/lib/eventCommentsApi'
import { commentFixture } from '../../eventRoles.fixture'
import { pendingConfirm, type PendingConfirm } from '../../shared/lib/confirm'
import { usePermissionsStore } from '../../stores/permissions'
import { reminderText } from '../lib/laterNotices'
import { attachmentFixture, channelFixture, fixtureId, FIXTURE_TIMESTAMP, messageFixture } from '../../test-fixtures.fixture'
import { useBlocksStore } from '../../stores/blocks'
import { useChatStore } from '../../stores/chat'
import { useLaterStore } from '../../stores/later'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { setLocale, t } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))
vi.mock('../../events/lib/eventCommentsApi', () => ({
  fetchComments: vi.fn(), postComment: vi.fn(), editComment: vi.fn(), deleteComment: vi.fn(), setCommentsMuted: vi.fn()
}))
vi.mock('../../events/lib/eventsDialog', () => ({ openEvents: vi.fn() }))
vi.mock('../../forum/lib/forumApi', () => ({
  updateForumPost: vi.fn(), markForumPostRead: vi.fn(), fetchForumTags: vi.fn(), fetchForumSettings: vi.fn()
}))

const BLOCKED = '00000000-0000-4000-8000-0000000000b1'
const WORDS = 'words of the blocked member'
const global = { directives: { tooltip: () => undefined } }

enableAutoUnmount(afterEach)
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  useServerFeaturesStore().list = ['blocks', 'reminders']
  useBlocksStore().blocked = [{ user_id: BLOCKED, username: 'troll', display_name: 'Troll', blocked_at: '2026-01-01T00:00:00Z' }]
})

const folded = (w: { find: (s: string) => { exists: () => boolean } }) => w.find('[data-testid="blocked-fold"]').exists()

describe('a blocked member is folded beyond the message list', () => {
  it('the block is in force (control)', () => {
    expect(useBlocksStore().hides({ id: 'm1', user_id: BLOCKED })).toBe(true)
  })

  it('the "replied to" line above a reply to the blocked member', () => {
    const reply = { id: 'm1', user_id: BLOCKED, username: 'troll', display_name: 'Troll', content: WORDS, deleted: false }
    const w = mount(ReplyPreview, { props: { reply }, global })
    expect(w.text()).not.toContain(WORDS)
    expect(folded(w)).toBe(true)
  })

  it('a search result written by the blocked member', () => {
    const msg = messageFixture({ id: 'm2', user_id: BLOCKED, username: 'troll', content: WORDS })
    const w = mount(SearchResultItem, { props: { index: 0, selected: false, voice: false, terms: ['words'], msg } })
    expect(w.text()).not.toContain(WORDS)
    // The option is the control (Enter shows it, SearchModal.test.ts): no button nested in it, and it says so.
    expect(w.find('button').exists()).toBe(false)
    expect(w.get('[role="option"]').text()).toContain(t('blocks.showWithEnter'))
  })

  it('the thread root of a search result, written by the blocked member', () => {
    const msg = messageFixture({ id: 'm3', content: 'a reply', parent_id: 'p1' })
    const root = { id: 'p1', channel_id: 'ch1', user_id: BLOCKED, username: 'troll', display_name: 'Troll', avatar_url: null, snippet: WORDS }
    const w = mount(SearchResultItem, { props: { index: 0, selected: false, voice: false, terms: ['reply'], msg, root } })
    expect(w.text()).not.toContain(WORDS)
    expect(w.text()).toContain('a reply')
  })

  it('a quote forwarded by another member', async () => {
    apiMock.mockResolvedValue({
      available: true, message_id: 'm4', channel_id: 'ch1', channel_name: 'general', content: WORDS, created_at: FIXTURE_TIMESTAMP,
      author: { id: BLOCKED, username: 'troll', display_name: 'Troll' }
    })
    const w = mount(ForwardedQuote, { props: { msg: messageFixture({ id: 'f1', content: '' }) }, global })
    await flushPromises()
    expect(w.text()).toContain(t('forward.label'))
    expect(w.text()).not.toContain(WORDS)
    expect(folded(w)).toBe(true)
  })

  it('the pin list', async () => {
    useChatStore().activeChannel = channelFixture({ id: fixtureId(2) })
    apiMock.mockResolvedValue([messageFixture({ id: 'm5', user_id: BLOCKED, content: WORDS, is_pinned: true })])
    const w = mount(PinnedMessages, { global })
    await w.get('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="pinned-panel"]').text()).not.toContain(WORDS)
    expect(folded(w)).toBe(true)
  })

  it('a saved message, which keeps its note and its remove button', () => {
    const entry = { available: true, message_id: 'm6', message: messageFixture({ id: 'm6', user_id: BLOCKED, content: WORDS }), note: 'my own note', saved_at: FIXTURE_TIMESTAMP }
    const w = mount(SavedEntry, { props: { entry }, global })
    expect(w.text()).not.toContain(WORDS)
    expect(w.text()).toContain('my own note')
    expect(w.find('[data-testid="saved-remove"]').exists()).toBe(true)
  })

  it('a forum post card: title, excerpt, picture and label', async () => {
    const post = messageFixture({
      id: 'm7', user_id: BLOCKED, title: 'title of the blocked member', content: WORDS,
      attachments: [attachmentFixture({ mime_type: 'image/png' })]
    })
    const w = mount(ForumPostCard, { props: { post } })
    expect(w.text()).not.toContain(WORDS)
    expect(w.text()).not.toContain('title of the blocked member')
    expect(w.find('[data-testid="forum-post-thumbnail"]').exists()).toBe(false)
    // Folded, the card is no button, so its Show is not nested in one, and nothing opens the post.
    const card = w.get('[data-testid="forum-post-card"]')
    expect(card.attributes('role')).toBeUndefined()
    expect(card.attributes('tabindex')).toBeUndefined()
    expect(card.attributes('aria-label')).toBeUndefined()
    await card.trigger('click')
    await card.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('select')).toBeUndefined()
  })

  it('a forum post card turns back into the button that opens the post, and takes the focus', async () => {
    const post = messageFixture({ id: 'm13', user_id: BLOCKED, title: 'title of the blocked member', content: WORDS })
    const w = mount(ForumPostCard, { props: { post }, attachTo: document.body })
    await w.get('[data-testid="blocked-message-show"]').trigger('click')
    await flushPromises()
    const card = w.get('[data-testid="forum-post-card"]')
    expect(card.attributes('role')).toBe('button')
    expect(card.attributes('aria-label')).toBe('title of the blocked member')
    expect(document.activeElement).toBe(card.element)
    await card.trigger('click')
    expect(w.emitted('select')).toHaveLength(1)
  })

  it('the title above an open forum post, which takes the focus when shown', async () => {
    useChatStore().activeChannel = channelFixture({ id: 'ch1', type: 'forum' })
    useChatStore().activeThread = messageFixture({ id: 'm8', channel_id: 'ch1', user_id: BLOCKED, title: 'title of the blocked member' })
    const w = mount(ForumPostHeader, { global, attachTo: document.body })
    expect(w.text()).not.toContain('title of the blocked member')
    expect(folded(w)).toBe(true)
    await w.get('[data-testid="blocked-message-show"]').trigger('click')
    await flushPromises()
    const title = w.get('[data-testid="forum-post-title"]').element as HTMLElement
    expect(document.activeElement).toBe(title)
    expect(title.tabIndex).toBe(-1)
  })

  it('an event comment, and its excerpt in the delete dialog', async () => {
    vi.spyOn(usePermissionsStore(), 'can').mockReturnValue(true)
    pendingConfirm.value = null
    const comment = commentFixture(3, { content: WORDS, user: { id: BLOCKED, username: 'troll', display_name: 'Troll' } })
    vi.mocked(eventApi.fetchComments).mockResolvedValue({ comments: [comment], has_more: false, muted: false })
    const w = mount(EventComments, { props: { eventId: fixtureId(1) }, global })
    await flushPromises()
    expect(w.get('[data-testid="event-comment"]').text()).not.toContain(WORDS)
    expect(folded(w)).toBe(true)
    await w.get('[data-testid="comment-delete"]').trigger('click')
    const asked = (): PendingConfirm | null => pendingConfirm.value
    expect(asked()?.excerpt).toBe(t('blocks.collapsed'))
    asked()?.resolve(false)
  })

  it('focus moves to the revealed content, not to the page', async () => {
    useChatStore().activeChannel = channelFixture({ id: fixtureId(2) })
    apiMock.mockResolvedValue([messageFixture({ id: 'm14', user_id: BLOCKED, content: WORDS, is_pinned: true })])
    const w = mount(PinnedMessages, { global, attachTo: document.body })
    await w.get('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="blocked-message-show"]').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w.get('[data-testid="pinned-item"]').element)
  })

  it('in the message list, where the row is replaced, focus moves to the message row', async () => {
    const msg = messageFixture({ id: 'm15', user_id: BLOCKED, content: WORDS })
    const blocks = useBlocksStore()
    const List = defineComponent(() => () => blocks.hides(msg)
      ? h(BlockedMessageRow, { msg })
      : h('div', { 'data-msg-id': msg.id, tabindex: -1 }, WORDS))
    const w = mount(List, { global, attachTo: document.body })
    await w.get('[data-testid="blocked-message-show"]').trigger('click')
    await flushPromises()
    expect(document.activeElement).toBe(w.get('[data-msg-id="m15"]').element)
  })

  it('a reminder about a message of the blocked member, in the list and in the notice', () => {
    const message = { id: 'm9', channel_id: 'ch1', parent_id: null, channel_name: 'general', dm: false, author: 'Troll', author_id: BLOCKED, snippet: WORDS }
    useLaterStore().reminders = [{ id: 'r1', remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: true, message }]
    const w = mount(LaterList, { global })
    expect(w.text()).not.toContain(WORDS)
    expect(folded(w)).toBe(true)
    expect(reminderText(message)).toBe(t('later.reminderDue'))
  })

  it('Show reveals that message everywhere for the session, and nothing else', async () => {
    const reply = { id: 'm10', user_id: BLOCKED, username: 'troll', display_name: 'Troll', content: WORDS, deleted: false }
    const line = mount(ReplyPreview, { props: { reply }, global })
    await line.get('[data-testid="blocked-message-show"]').trigger('click')
    expect(line.text()).toContain(WORDS)
    const same = mount(SearchResultItem, { props: { index: 0, selected: false, voice: false, terms: [], msg: messageFixture({ id: 'm10', user_id: BLOCKED, content: WORDS }) } })
    expect(same.text()).toContain(WORDS)
    const other = mount(SearchResultItem, { props: { index: 0, selected: false, voice: false, terms: [], msg: messageFixture({ id: 'm11', user_id: BLOCKED, content: WORDS }) } })
    expect(other.text()).not.toContain(WORDS)
  })

  it('a member who is not blocked is shown as before', () => {
    const reply = { id: 'm12', user_id: fixtureId(7), username: 'ada', display_name: 'Ada', content: 'plain words', deleted: false }
    const w = mount(ReplyPreview, { props: { reply }, global })
    expect(w.text()).toContain('plain words')
    expect(folded(w)).toBe(false)
  })
})
