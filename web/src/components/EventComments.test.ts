import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventComments from './EventComments.vue'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useEventCommentsStore } from '../stores/eventComments'
import { pendingConfirm } from '../lib/confirm'
import * as api from '../lib/eventCommentsApi'
import { setLocale } from '../i18n'
import { commentFixture } from '../eventRoles.fixture'
import { fixtureId, userFixture } from '../test-fixtures.fixture'

vi.mock('../lib/eventCommentsApi', () => ({
  fetchComments: vi.fn(), postComment: vi.fn(), editComment: vi.fn(), deleteComment: vi.fn(), setCommentsMuted: vi.fn()
}))
vi.mock('../lib/eventsDialog', () => ({ openEvents: vi.fn() }))

let wrapper: VueWrapper
const mine = commentFixture(1, { content: 'My words', edited_at: '2026-11-01T10:00:00Z' })
const theirs = commentFixture(2, { content: 'Their words' })

async function mountComments(comments = [mine, theirs], hasMore = false, muted = false) {
  vi.mocked(api.fetchComments).mockResolvedValue({ comments, has_more: hasMore, muted })
  wrapper = mount(EventComments, { props: { eventId: fixtureId(1) } })
  await flushPromises()
  return wrapper
}

const items = () => wrapper.findAll('[data-testid="event-comment"]')

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
  useAuthStore().user = userFixture({ id: fixtureId(1) })
  pendingConfirm.value = null
})
afterEach(() => wrapper?.unmount())

describe('EventComments', () => {
  it('lists the comments with author, edit mark and the actions the member may use', async () => {
    await mountComments()
    expect(items()).toHaveLength(2)
    expect(items()[0]!.text()).toContain('User 1')
    expect(items()[0]!.text()).toContain('(edited)')
    expect(items()[0]!.find('[data-testid="comment-edit"]').exists()).toBe(true)
    expect(items()[0]!.find('[data-testid="comment-delete"]').exists()).toBe(true)
    expect(items()[1]!.find('[data-testid="comment-edit"]').exists()).toBe(false)
    expect(items()[1]!.find('[data-testid="comment-delete"]').exists()).toBe(false)
  })

  it('lets a moderator delete any comment but edit none', async () => {
    vi.spyOn(usePermissionsStore(), 'can').mockImplementation(key => key === 'manage_messages')
    await mountComments()
    expect(items()[1]!.find('[data-testid="comment-delete"]').exists()).toBe(true)
    expect(items()[1]!.find('[data-testid="comment-edit"]').exists()).toBe(false)
  })

  it('shows a hint without comments and an alert when they cannot be loaded', async () => {
    await mountComments([])
    expect(wrapper.text()).toContain('No comments yet')
    wrapper.unmount()
    vi.mocked(api.fetchComments).mockRejectedValue(new Error('down'))
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    wrapper = mount(EventComments, { props: { eventId: fixtureId(1) } })
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Couldn’t load the comments')
  })

  it('sends with Enter, keeps Shift+Enter for a new line and clears the box', async () => {
    await mountComments([])
    vi.mocked(api.postComment).mockResolvedValue(mine)
    const input = wrapper.get<HTMLTextAreaElement>('[data-testid="comment-input"]')
    expect(wrapper.get('[data-testid="comment-send"]').attributes('disabled')).toBeDefined()
    await input.setValue('Hello')
    await input.trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(api.postComment).not.toHaveBeenCalled()
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(api.postComment).toHaveBeenCalledWith(fixtureId(1), 'Hello')
    expect(input.element.value).toBe('')
    expect(items()).toHaveLength(1)
    await input.trigger('keydown', { key: 'Enter' }) // empty: nothing to send
    expect(api.postComment).toHaveBeenCalledTimes(1)
  })

  it('keeps the text when sending fails', async () => {
    await mountComments([])
    vi.mocked(api.postComment).mockRejectedValue(new Error('slow down'))
    const input = wrapper.get<HTMLTextAreaElement>('[data-testid="comment-input"]')
    await input.setValue('Hello')
    await wrapper.get('form:last-of-type').trigger('submit')
    await flushPromises()
    expect(input.element.value).toBe('Hello')
  })

  it('edits an own comment inline and can cancel with Escape', async () => {
    await mountComments()
    vi.mocked(api.editComment).mockResolvedValue({ ...mine, content: 'Better words' })
    await items()[0]!.get('[data-testid="comment-edit"]').trigger('click')
    const box = items()[0]!.get<HTMLTextAreaElement>('[data-testid="comment-edit-input"]')
    expect(box.element.value).toBe('My words')
    await box.trigger('keydown', { key: 'Escape' })
    expect(items()[0]!.find('[data-testid="comment-edit-input"]').exists()).toBe(false)

    await items()[0]!.get('[data-testid="comment-edit"]').trigger('click')
    await items()[0]!.get('[data-testid="comment-edit-input"]').setValue('Better words')
    await items()[0]!.get('form').trigger('submit')
    await flushPromises()
    expect(api.editComment).toHaveBeenCalledWith(fixtureId(1), mine.id, 'Better words')
    expect(items()[0]!.text()).toContain('Better words')
  })

  it('asks before deleting and deletes on confirmation', async () => {
    await mountComments()
    vi.mocked(api.deleteComment).mockResolvedValue()
    await items()[0]!.get('[data-testid="comment-delete"]').trigger('click')
    expect(pendingConfirm.value?.title).toBe('Delete this comment?')
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(api.deleteComment).not.toHaveBeenCalled()

    await items()[0]!.get('[data-testid="comment-delete"]').trigger('click')
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(api.deleteComment).toHaveBeenCalledWith(fixtureId(1), mine.id)
    expect(items()).toHaveLength(1)
  })

  it('loads older comments on request and toggles the notices of the event', async () => {
    await mountComments([theirs], true)
    vi.mocked(api.fetchComments).mockResolvedValue({ comments: [mine], has_more: false, muted: false })
    await wrapper.get('[data-testid="comments-older"]').trigger('click')
    await flushPromises()
    expect(items()).toHaveLength(2)
    expect(wrapper.find('[data-testid="comments-older"]').exists()).toBe(false)

    vi.mocked(api.setCommentsMuted).mockResolvedValue(true)
    const bell = wrapper.get('[data-testid="comments-mute"]')
    expect(bell.attributes('aria-pressed')).toBe('false')
    await bell.trigger('click')
    await flushPromises()
    expect(api.setCommentsMuted).toHaveBeenCalledWith(fixtureId(1), true)
    expect(bell.attributes('aria-pressed')).toBe('true')
  })

  it('follows a comment another member wrote while it is open', async () => {
    await mountComments([mine])
    useEventCommentsStore().handleChange({ action: 'created', event_id: fixtureId(1), comment_id: theirs.id, comment: theirs })
    await flushPromises()
    expect(items()).toHaveLength(2)
    useEventCommentsStore().handleChange({ action: 'deleted', event_id: fixtureId(1), comment_id: theirs.id })
    await flushPromises()
    expect(items()).toHaveLength(1)
  })

  it('shows the counter near the limit', async () => {
    await mountComments([])
    await wrapper.get('[data-testid="comment-input"]').setValue('x'.repeat(1700))
    expect(wrapper.text()).toContain('1700/2000')
  })
})
