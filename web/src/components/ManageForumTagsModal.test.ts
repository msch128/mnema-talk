import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ManageForumTagsModal from './ManageForumTagsModal.vue'
import { setLocale } from '../i18n'
import * as forumApi from '../lib/forumApi'
import { pendingConfirm } from '../lib/confirm'
import { ApiError } from '../lib/api'
import type { ForumTag } from '../types/domain'
import { forumTagFixture } from '../test-fixtures.fixture'

vi.mock('../lib/forumApi', () => ({
  createForumTag: vi.fn(),
  updateForumTag: vi.fn(),
  deleteForumTag: vi.fn(),
  reorderForumTags: vi.fn(),
  updateForumSettings: vi.fn()
}))

let wrapper: VueWrapper

function tag(i: number, emoji?: string, extra: Partial<ForumTag> = {}): ForumTag {
  return forumTagFixture({ id: `tag-${i}`, channel_id: 'chan-1', name: `tag${i}`, position: i, ...(emoji ? { emoji } : {}), ...extra })
}

const noRules = { require_tag: false, guidelines: '' }

function mountModal(tags: ForumTag[] = [tag(1, '💬'), tag(2)], settings = noRules) {
  wrapper = mount(ManageForumTagsModal, { props: { channelId: 'chan-1', tags, settings }, attachTo: document.body })
  return wrapper
}

const rows = () => wrapper.findAll('[data-testid="forum-tag-row"]')
const editor = () => wrapper.find('[data-testid="forum-tag-editor"]')
const nameInput = () => wrapper.get<HTMLInputElement>('[data-testid="forum-tag-name"]')
const emojiInput = () => wrapper.get<HTMLInputElement>('input[placeholder="Emoji"]')
const escape = () => document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))

async function answerConfirm(yes: boolean) {
  await flushPromises()
  pendingConfirm.value?.resolve(yes)
  await flushPromises()
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
})

afterEach(() => {
  pendingConfirm.value = null
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('ManageForumTagsModal', () => {
  it('lists the tags with a counter, a lock on moderated ones and an empty state', async () => {
    mountModal([tag(1, '💬'), tag(2, undefined, { moderated: true })])
    expect(rows()).toHaveLength(2)
    expect(wrapper.text()).toContain('tag1')
    expect(wrapper.text()).toContain('💬')
    expect(wrapper.text()).toContain('2/20')
    expect(rows()[0]!.find('svg[aria-label="Only moderators can apply this tag"]').exists()).toBe(false)
    expect(rows()[1]!.find('svg[aria-label="Only moderators can apply this tag"]').exists()).toBe(true)
    await wrapper.setProps({ tags: [] })
    expect(wrapper.text()).toContain('No tags created yet.')
  })

  it('Escape while editing a tag leaves edit mode and keeps the dialog open', async () => {
    mountModal()
    await wrapper.get('button[aria-label="Edit"]').trigger('click')
    expect(editor().exists()).toBe(true)
    escape()
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(editor().exists()).toBe(false)
    escape()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('adds a trimmed tag with colour and moderated flag and reports the change', async () => {
    vi.mocked(forumApi.createForumTag).mockResolvedValue(tag(3))
    mountModal()
    await wrapper.get('[data-testid="forum-add-tag"]').trigger('click')
    await emojiInput().setValue(' 🔥 ')
    await nameInput().setValue('  hot  ')
    await wrapper.get('button[aria-label="Red"]').trigger('click')
    await wrapper.get('[data-testid="forum-tag-moderated"]').trigger('click')
    await wrapper.get('[data-testid="forum-tag-save"]').trigger('click')
    await flushPromises()

    expect(forumApi.createForumTag).toHaveBeenCalledWith('chan-1', 'hot', '🔥', { color: 'red', moderated: true })
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(editor().exists()).toBe(false)
  })

  it('adds a plain tag without an emoji', async () => {
    vi.mocked(forumApi.createForumTag).mockResolvedValue(tag(3))
    mountModal()
    await wrapper.get('[data-testid="forum-add-tag"]').trigger('click')
    await nameInput().setValue('plain')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(forumApi.createForumTag).toHaveBeenCalledWith('chan-1', 'plain', undefined, { color: '', moderated: false })
  })

  it('does not save while the name is blank', async () => {
    mountModal()
    await wrapper.get('[data-testid="forum-add-tag"]').trigger('click')
    await nameInput().setValue('   ')
    expect(wrapper.get('[data-testid="forum-tag-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(forumApi.createForumTag).not.toHaveBeenCalled()
  })

  it('shows the server message when adding fails and keeps the form', async () => {
    vi.mocked(forumApi.createForumTag).mockRejectedValue(new ApiError(409, 'CONFLICT', 'Tag exists'))
    mountModal()
    await wrapper.get('[data-testid="forum-add-tag"]').trigger('click')
    await nameInput().setValue('dup')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Tag exists')
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(nameInput().element.value).toBe('dup')
  })

  it('falls back to a generic message for unknown add errors', async () => {
    vi.mocked(forumApi.createForumTag).mockRejectedValue('boom')
    mountModal()
    await wrapper.get('[data-testid="forum-add-tag"]').trigger('click')
    await nameInput().setValue('x')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Couldn’t create the tag.')
  })

  it('offers no add button at the 20 tag limit', () => {
    mountModal(Array.from({ length: 20 }, (_, i) => tag(i + 1)))
    expect(wrapper.text()).toContain('Maximum number of tags reached')
    expect(wrapper.find('[data-testid="forum-add-tag"]').exists()).toBe(false)
  })

  it('edits a tag in place, sending name, emoji, colour and moderated flag', async () => {
    vi.mocked(forumApi.updateForumTag).mockResolvedValue(tag(1))
    mountModal([tag(1, '💬', { color: 'blue' }), tag(2)])
    await wrapper.get('button[aria-label="Edit"]').trigger('click')
    expect(nameInput().element.value).toBe('tag1')
    expect(emojiInput().element.value).toBe('💬')
    expect(wrapper.get('button[aria-label="Blue"]').attributes('aria-checked')).toBe('true')
    await nameInput().setValue(' renamed ')
    await emojiInput().setValue('')
    await wrapper.get('button[aria-label="No colour"]').trigger('click')
    await wrapper.get('[data-testid="forum-tag-moderated"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(forumApi.updateForumTag).toHaveBeenCalledWith('chan-1', 'tag-1', 'renamed', '', { color: '', moderated: true })
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(editor().exists()).toBe(false)
  })

  it('cancels an edit with the cancel button without saving', async () => {
    mountModal()
    await wrapper.get('button[aria-label="Edit"]').trigger('click')
    await editor().findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(editor().exists()).toBe(false)
    expect(forumApi.updateForumTag).not.toHaveBeenCalled()
  })

  it('keeps editing and shows an error when saving fails', async () => {
    vi.mocked(forumApi.updateForumTag).mockRejectedValue(new Error('nope'))
    mountModal()
    await wrapper.get('button[aria-label="Edit"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('nope')
    expect(editor().exists()).toBe(true)
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

  it('asks before deleting a tag and deletes it after a yes', async () => {
    vi.mocked(forumApi.deleteForumTag).mockResolvedValue(undefined)
    mountModal()
    await wrapper.get('button[aria-label="Delete"]').trigger('click')
    await flushPromises()
    expect(pendingConfirm.value?.excerpt).toBe('tag1')
    expect(forumApi.deleteForumTag).not.toHaveBeenCalled()
    await answerConfirm(true)
    expect(forumApi.deleteForumTag).toHaveBeenCalledWith('chan-1', 'tag-1')
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('keeps the tag when the confirmation is declined', async () => {
    mountModal()
    await wrapper.get('button[aria-label="Delete"]').trigger('click')
    await answerConfirm(false)
    expect(forumApi.deleteForumTag).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

  it('shows an error when deleting fails', async () => {
    vi.mocked(forumApi.deleteForumTag).mockRejectedValue(new Error('cannot delete'))
    mountModal()
    await wrapper.get('button[aria-label="Delete"]').trigger('click')
    await answerConfirm(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('cannot delete')
  })

  it('moves a tag by sending the whole order with the two ids swapped', async () => {
    vi.mocked(forumApi.reorderForumTags).mockResolvedValue([])
    mountModal([tag(1), tag(2), tag(3)])
    expect(rows()[0]!.get('button[aria-label="Move up"]').attributes('disabled')).toBeDefined()
    expect(rows()[2]!.get('button[aria-label="Move down"]').attributes('disabled')).toBeDefined()

    await rows()[1]!.get('button[aria-label="Move up"]').trigger('click')
    await flushPromises()
    expect(forumApi.reorderForumTags).toHaveBeenLastCalledWith('chan-1', ['tag-2', 'tag-1', 'tag-3'])

    await rows()[1]!.get('button[aria-label="Move down"]').trigger('click')
    await flushPromises()
    expect(forumApi.reorderForumTags).toHaveBeenLastCalledWith('chan-1', ['tag-1', 'tag-3', 'tag-2'])
    expect(wrapper.emitted('updated')).toHaveLength(2)
  })

  it('saves the rules and passes the new settings up', async () => {
    const saved = { require_tag: true, guidelines: 'Be kind.' }
    vi.mocked(forumApi.updateForumSettings).mockResolvedValue(saved)
    mountModal()
    expect(wrapper.get('[data-testid="forum-settings-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="forum-require-tag"]').trigger('click')
    await wrapper.get('#forum-guidelines').setValue('  Be kind.  ')
    await wrapper.get('[data-testid="forum-settings-save"]').trigger('click')
    await flushPromises()
    expect(forumApi.updateForumSettings).toHaveBeenCalledWith('chan-1', { require_tag: true, guidelines: 'Be kind.' })
    expect(wrapper.emitted('settings-saved')![0]).toEqual([saved])
  })

  it('emits close from the close button', async () => {
    mountModal()
    await wrapper.findAll('button').find(b => b.text() === 'Close')!.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
