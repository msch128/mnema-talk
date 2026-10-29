import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import CreateForumPostModal from './CreateForumPostModal.vue'
import { setLocale } from '../i18n'
import * as forumApi from '../lib/forumApi'
import type { ForumTag } from '../types/domain'
import { messageFixture, forumTagFixture } from '../test-fixtures.fixture'

vi.mock('../lib/forumApi', () => ({
  createForumPost: vi.fn(),
  createForumPostWithFile: vi.fn(),
  uploadForumPostFile: vi.fn(),
  fetchForumPosts: vi.fn(),
  fetchForumTags: vi.fn(),
  updateForumPost: vi.fn()
}))

let wrapper: VueWrapper

const names: Array<[string, string]> = [['announcements', '📢'], ['help', '❓'], ['feedback', '💡'], ['bugs', '🐛'], ['general', '💬'], ['extra', '✨']]
const sampleTags: ForumTag[] = names.map(([name, emoji], i) => forumTagFixture({ id: `tag-${i + 1}`, channel_id: 'chan-1', name, emoji, position: i + 1 }))

interface Options { tags?: ForumTag[]; settings?: { require_tag: boolean; guidelines: string }; canModerate?: boolean; canAttach?: boolean }

function mountModal({ tags = sampleTags, settings = { require_tag: false, guidelines: '' }, canModerate = false, canAttach = true }: Options = {}) {
  wrapper = mount(CreateForumPostModal, {
    props: { channelId: 'chan-1', availableTags: tags, settings, canModerate, canAttach },
    attachTo: document.body
  })
  return wrapper
}

async function fill(w: VueWrapper) {
  await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
  await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')
}

async function pick(w: VueWrapper, files: File[]) {
  const input = w.get<HTMLInputElement>('input[type="file"]')
  Object.defineProperty(input.element, 'files', { value: files, configurable: true })
  await input.trigger('change')
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
})

afterEach(() => {
  wrapper?.unmount()
})

describe('CreateForumPostModal', () => {
  it('disables submit when title or content is empty', async () => {
    const w = mountModal()
    const submitBtn = w.find('button[type="submit"]')
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
    expect(submitBtn.attributes('disabled')).toBeDefined()

    await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')
    expect(submitBtn.attributes('disabled')).toBeUndefined()
  })

  it('names the tag group for assistive technology', () => {
    const w = mountModal()
    const group = w.get('[role="group"]')
    const label = w.get(`#${group.attributes('aria-labelledby')}`)
    expect(label.text()).not.toBe('')
    expect(group.findAll('[data-testid="tag-toggle-btn"]')).toHaveLength(6)
  })

  it('allows selecting up to 5 tags and disables further selections', async () => {
    const w = mountModal()
    const tagButtons = w.findAll('[data-testid="tag-toggle-btn"]')
    expect(tagButtons).toHaveLength(6)

    // Select 5 tags
    for (let i = 0; i < 5; i++) {
      await tagButtons[i]!.trigger('click')
      expect(tagButtons[i]!.attributes('aria-pressed')).toBe('true')
    }

    // 6th tag button should be disabled
    expect(tagButtons[5]!.attributes('disabled')).toBeDefined()

    // Unselect 1 tag
    await tagButtons[0]!.trigger('click')
    expect(tagButtons[0]!.attributes('aria-pressed')).toBe('false')
    expect(tagButtons[5]!.attributes('disabled')).toBeUndefined()
  })

  it('creates post successfully and emits events', async () => {
    const mockPost = messageFixture({
      id: 'post-123',
      channel_id: 'chan-1',
      title: 'My First Post',
      content: 'Here is the first message body.'
    })
    vi.mocked(forumApi.createForumPost).mockResolvedValueOnce(mockPost)

    const w = mountModal()
    await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
    await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')

    const tagButtons = w.findAll('[data-testid="tag-toggle-btn"]')
    await tagButtons[0]!.trigger('click')

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(forumApi.createForumPost).toHaveBeenCalledWith(
      'chan-1',
      'My First Post',
      'Here is the first message body.',
      ['tag-1']
    )
    expect(w.emitted('created')).toBeTruthy()
    expect(w.emitted('created')![0]![0]).toEqual(mockPost)
    expect(w.emitted('close')).toBeTruthy()
  })

  it('displays error on creation failure', async () => {
    vi.mocked(forumApi.createForumPost).mockRejectedValueOnce(new Error('Server error'))

    const w = mountModal()
    await w.find<HTMLInputElement>('#post-title').setValue('My First Post')
    await w.find<HTMLTextAreaElement>('#post-content').setValue('Here is the first message body.')

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(w.text()).toContain('Server error')
    expect(w.get('[role="alert"]').text()).toContain('Server error')
    expect(w.emitted('created')).toBeFalsy()
  })

  it('shows the forum guidelines above the form', () => {
    const w = mountModal({ settings: { require_tag: false, guidelines: 'Search before you ask.' } })
    expect(w.get('[data-testid="forum-guidelines"]').text()).toContain('Search before you ask.')
    wrapper.unmount()
    expect(mountModal().find('[data-testid="forum-guidelines"]').exists()).toBe(false)
  })

  it('needs a tag when the forum requires one, unless the member can apply none', async () => {
    const required = { require_tag: true, guidelines: '' }
    const w = mountModal({ settings: required })
    await fill(w)
    expect(w.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await w.findAll('[data-testid="tag-toggle-btn"]')[0]!.trigger('click')
    expect(w.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()

    // Only a moderated tag exists: a member cannot apply it, so nothing is required of them.
    const locked = mountModal({ settings: required, tags: [forumTagFixture({ id: 'm', moderated: true })] })
    await fill(locked)
    expect(locked.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    expect(locked.get('[data-testid="tag-toggle-btn"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()

    const moderator = mountModal({ settings: required, canModerate: true, tags: [forumTagFixture({ id: 'm', moderated: true })] })
    await fill(moderator)
    expect(moderator.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(moderator.get('[data-testid="tag-toggle-btn"]').attributes('disabled')).toBeUndefined()
  })

  it('sends the first file with the post and the others as replies of the post', async () => {
    const post = messageFixture({ id: 'post-9', title: 'My First Post' })
    vi.mocked(forumApi.createForumPostWithFile).mockResolvedValueOnce(post)
    vi.mocked(forumApi.uploadForumPostFile).mockResolvedValue(messageFixture())
    const w = mountModal()
    await fill(w)
    await w.findAll('[data-testid="tag-toggle-btn"]')[1]!.trigger('click')
    const first = new File(['1'], 'one.png', { type: 'image/png' })
    const second = new File(['2'], 'two.png', { type: 'image/png' })
    await pick(w, [first, second, new File(['x'], 'doc.pdf', { type: 'application/pdf' })])
    expect(w.findAll('[data-testid="post-files"] li')).toHaveLength(2)

    await w.find('form').trigger('submit')
    await flushPromises()

    expect(forumApi.createForumPost).not.toHaveBeenCalled()
    expect(forumApi.createForumPostWithFile).toHaveBeenCalledWith(
      'chan-1', { title: 'My First Post', content: 'Here is the first message body.', tagIds: ['tag-2'] }, first)
    expect(forumApi.uploadForumPostFile).toHaveBeenCalledTimes(1)
    expect(forumApi.uploadForumPostFile).toHaveBeenCalledWith('chan-1', 'post-9', second)
    expect(w.emitted('created')![0]![0]).toEqual(post)
  })

  it('keeps at most four files, lets one be removed and hides the button without Attach files', async () => {
    const w = mountModal()
    await pick(w, Array.from({ length: 6 }, (_, i) => new File(['x'], `f${i}.png`, { type: 'image/png' })))
    expect(w.findAll('[data-testid="post-files"] li')).toHaveLength(4)
    expect(w.get('[data-testid="post-attach"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="post-files"] li button').trigger('click')
    expect(w.findAll('[data-testid="post-files"] li')).toHaveLength(3)
    wrapper.unmount()
    expect(mountModal({ canAttach: false }).find('[data-testid="post-attach"]').exists()).toBe(false)
  })

  it('skips non-media files and files over the upload limit, each with a toast', async () => {
    const w = mountModal()
    usePermissionsStore().info = { max_upload_mb: 1 } as never
    const big = new File(['x'], 'big.png', { type: 'image/png' })
    Object.defineProperty(big, 'size', { value: 2 * 1024 * 1024 })
    await pick(w, [new File(['x'], 'doc.pdf', { type: 'application/pdf' }), big, new File(['x'], 'ok.png', { type: 'image/png' })])
    expect(w.findAll('[data-testid="post-files"] li').map(li => li.text())).toEqual(['ok.png'])
    const toasts = useToastStore().toasts.map(x => x.text).join('|')
    expect(toasts).toContain('images and videos')
    expect(toasts).toContain('too large')
  })

  it('still creates the post when a further file fails, and says so', async () => {
    const post = messageFixture({ id: 'post-9' })
    vi.mocked(forumApi.createForumPostWithFile).mockResolvedValueOnce(post)
    vi.mocked(forumApi.uploadForumPostFile).mockRejectedValueOnce(new Error('storage down'))
    const w = mountModal()
    await fill(w)
    await pick(w, [new File(['1'], 'a.png', { type: 'image/png' }), new File(['2'], 'b.png', { type: 'image/png' })])
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.map(x => x.text).join('|')).toContain('storage down')
    expect(w.emitted('created')).toHaveLength(1)
    expect(w.emitted('close')).toBeTruthy()
  })

  it('shows the error and keeps the dialog when the post with a file fails', async () => {
    vi.mocked(forumApi.createForumPostWithFile).mockRejectedValueOnce(new Error('too big'))
    const w = mountModal()
    await fill(w)
    await pick(w, [new File(['1'], 'a.png', { type: 'image/png' })])
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('too big')
    expect(w.emitted('close')).toBeUndefined()
    expect(w.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
  })

  it('opens the file picker from the attach button, inserts a picked emoji and ignores a submit that is not valid', async () => {
    const w = mountModal()
    const input = w.get<HTMLInputElement>('input[type="file"]')
    const click = vi.spyOn(input.element, 'click')
    await w.get('[data-testid="post-attach"]').trigger('click')
    expect(click).toHaveBeenCalled()

    await w.find<HTMLTextAreaElement>('#post-content').setValue('hi ')
    w.findComponent({ name: 'EmojiButton' }).vm.$emit('pick', '🎉')
    await flushPromises()
    expect(w.find<HTMLTextAreaElement>('#post-content').element.value).toContain('🎉')

    await w.find('form').trigger('submit')
    expect(forumApi.createForumPost).not.toHaveBeenCalled()
  })
})
