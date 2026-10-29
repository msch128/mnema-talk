import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import ManageForumTagsModal from './ManageForumTagsModal.vue'
import { setLocale } from '../i18n'
import * as forumApi from '../lib/forumApi'
import { ApiError } from '../lib/api'
import type { ForumTag } from '../types/domain'

vi.mock('../lib/forumApi', () => ({
  createForumTag: vi.fn(),
  updateForumTag: vi.fn(),
  deleteForumTag: vi.fn()
}))

let wrapper: VueWrapper

function tag(i: number, emoji?: string): ForumTag {
  return { id: `tag-${i}`, channel_id: 'chan-1', name: `tag${i}`, ...(emoji ? { emoji } : {}), created_at: '2026-01-01T00:00:00Z' }
}

function mountModal(tags: ForumTag[] = [tag(1, '💬'), tag(2)]) {
  wrapper = mount(ManageForumTagsModal, { props: { channelId: 'chan-1', tags }, attachTo: document.body })
  return wrapper
}

const addButton = () => wrapper.findAll('button').find(b => b.text() === 'Add')!
const inputs = () => wrapper.findAll<HTMLInputElement>('input')

beforeEach(() => {
  setLocale('en')
  vi.clearAllMocks()
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('ManageForumTagsModal', () => {
  it('lists the tags with a counter and an empty state', async () => {
    mountModal()
    expect(wrapper.text()).toContain('tag1')
    expect(wrapper.text()).toContain('💬')
    expect(wrapper.text()).toContain('2/20')
    await wrapper.setProps({ tags: [] })
    expect(wrapper.text()).toContain('No tags created yet.')
  })

  it('Escape while editing a tag leaves edit mode and keeps the dialog open', async () => {
    mountModal()
    await wrapper.get('button[aria-label="Edit"]').trigger('click')
    expect(inputs()).toHaveLength(4)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(inputs()).toHaveLength(2)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('names the icon buttons in edit mode', async () => {
    mountModal()
    await wrapper.get('button[aria-label="Edit"]').trigger('click')
    expect(wrapper.find('button[aria-label="Save"]').exists()).toBe(true)
    expect(wrapper.find('button[aria-label="Cancel"]').exists()).toBe(true)
  })

  it('adds a trimmed tag, clears the form and reports the change', async () => {
    vi.mocked(forumApi.createForumTag).mockResolvedValue(tag(3))
    mountModal()
    const [emoji, name] = inputs()
    await emoji!.setValue(' 🔥 ')
    await name!.setValue('  hot  ')
    await addButton().trigger('click')
    await flushPromises()

    expect(forumApi.createForumTag).toHaveBeenCalledWith('chan-1', 'hot', '🔥')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(name!.element.value).toBe('')
    expect(emoji!.element.value).toBe('')
  })

  it('adds a tag without an emoji and via the Enter key', async () => {
    vi.mocked(forumApi.createForumTag).mockResolvedValue(tag(3))
    mountModal()
    const name = inputs()[1]!
    await name.setValue('plain')
    await name.trigger('keydown.enter')
    await flushPromises()
    expect(forumApi.createForumTag).toHaveBeenCalledWith('chan-1', 'plain', undefined)
  })

  it('does not add while the name is blank', async () => {
    mountModal()
    expect(addButton().attributes('disabled')).toBeDefined()
    await inputs()[1]!.setValue('   ')
    await inputs()[1]!.trigger('keydown.enter')
    expect(forumApi.createForumTag).not.toHaveBeenCalled()
  })

  it('shows the server message when adding fails', async () => {
    vi.mocked(forumApi.createForumTag).mockRejectedValue(new ApiError(409, 'CONFLICT', 'Tag exists'))
    mountModal()
    await inputs()[1]!.setValue('dup')
    await addButton().trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Tag exists')
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(inputs()[1]!.element.value).toBe('dup')
  })

  it('falls back to a generic message for unknown add errors', async () => {
    vi.mocked(forumApi.createForumTag).mockRejectedValue('boom')
    mountModal()
    await inputs()[1]!.setValue('x')
    await addButton().trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Couldn’t create the tag.')
  })

  it('hides the add form at the 20 tag limit', () => {
    mountModal(Array.from({ length: 20 }, (_, i) => tag(i)))
    expect(wrapper.text()).toContain('Maximum number of tags reached')
    expect(wrapper.find('input').exists()).toBe(false)
  })

  it('edits a tag in place and saves it', async () => {
    vi.mocked(forumApi.updateForumTag).mockResolvedValue(tag(1))
    mountModal()
    await wrapper.find('button[title="Edit"]').trigger('click')
    const [, , emoji, name] = inputs()
    expect(name!.element.value).toBe('tag1')
    expect(emoji!.element.value).toBe('💬')
    await name!.setValue(' renamed ')
    await emoji!.setValue('')
    await name!.trigger('keydown.enter')
    await flushPromises()

    expect(forumApi.updateForumTag).toHaveBeenCalledWith('chan-1', 'tag-1', 'renamed', '')
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(inputs()).toHaveLength(2) // back to the add form only
    expect(wrapper.text()).not.toContain('renamed') // parent owns the list
  })

  it('saves an edit through the confirm button and cancels with Escape or the cancel button', async () => {
    vi.mocked(forumApi.updateForumTag).mockResolvedValue(tag(1))
    mountModal()
    await wrapper.find('button[title="Edit"]').trigger('click')
    expect(inputs()).toHaveLength(4)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(inputs()).toHaveLength(2)

    await wrapper.find('button[title="Edit"]').trigger('click')
    await wrapper.get('button[aria-label="Cancel"]').trigger('click')
    expect(inputs()).toHaveLength(2)

    await wrapper.find('button[title="Edit"]').trigger('click')
    await wrapper.get('button[aria-label="Save"]').trigger('click')
    await flushPromises()
    expect(forumApi.updateForumTag).toHaveBeenCalledWith('chan-1', 'tag-1', 'tag1', '💬')
  })

  it('refuses to save an empty name', async () => {
    mountModal()
    await wrapper.find('button[title="Edit"]').trigger('click')
    const name = inputs()[3]!
    await name.setValue('   ')
    await name.trigger('keydown.enter')
    expect(forumApi.updateForumTag).not.toHaveBeenCalled()
  })

  it('keeps editing and shows an error when saving fails', async () => {
    vi.mocked(forumApi.updateForumTag).mockRejectedValue(new Error('nope'))
    mountModal()
    await wrapper.find('button[title="Edit"]').trigger('click')
    await inputs()[3]!.trigger('keydown.enter')
    await flushPromises()
    expect(wrapper.text()).toContain('nope')
    expect(inputs()).toHaveLength(4)
    expect(wrapper.emitted('updated')).toBeUndefined()
  })

  it('deletes a tag and reports the change', async () => {
    vi.mocked(forumApi.deleteForumTag).mockResolvedValue(undefined)
    mountModal()
    await wrapper.find('button[title="Delete"]').trigger('click')
    await flushPromises()
    expect(forumApi.deleteForumTag).toHaveBeenCalledWith('chan-1', 'tag-1')
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('shows an error when deleting fails and ignores a second click while deleting', async () => {
    let reject!: (e: unknown) => void
    vi.mocked(forumApi.deleteForumTag).mockReturnValue(new Promise((_, r) => { reject = r }))
    mountModal()
    const buttons = wrapper.findAll('button[title="Delete"]')
    await buttons[0]!.trigger('click')
    await buttons[1]!.trigger('click')
    expect(forumApi.deleteForumTag).toHaveBeenCalledTimes(1)
    reject(new Error('cannot delete'))
    await flushPromises()
    expect(wrapper.text()).toContain('cannot delete')
  })

  it('emits close from the close button', async () => {
    mountModal()
    await wrapper.findAll('button').find(b => b.text() === 'Close')!.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
