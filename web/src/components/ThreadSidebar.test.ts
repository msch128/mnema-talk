import type { Message } from '../types/domain'
import { requireValue } from '../test-fixtures.fixture'
import { messageFixture, userFixture, channelFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import ThreadSidebar from './ThreadSidebar.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { setLocale, t } from '../i18n'

let w!: ReturnType<typeof mount<typeof ThreadSidebar>>

function msg(id: string, user: string, extra: Partial<Message> = {}) {
  return messageFixture({
    id, channel_id: 'ch1', user_id: user, username: user, display_name: user.toUpperCase(),
    content: 'text ' + id, created_at: '2026-01-01T10:00:00Z', attachments: [], reactions: [], ...extra
  })
}

function setup() {
  useAuthStore().user = userFixture({ id: 'me', username: 'me', role: 'user' })
  const chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'allgemein', type: 'text' })
  chat.activeThread = msg('root', 'u2')
  chat.threadReplies = [msg('r1', 'u2'), msg('r2', 'me'), msg('r3', 'u2')]
  w = mount(ThreadSidebar, { attachTo: document.body })
  return chat
}

const key = (el: { element: Element }, init: KeyboardEventInit) => {
  const ev = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
  el.element.dispatchEvent(ev)
  return ev
}

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
})

afterEach(() => {
  w?.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ThreadSidebar keyboard', () => {
  it('replies are focusable and show their actions on focus', () => {
    setup()
    const row = w.find<HTMLElement>('[data-reply-id="r1"]')
    expect(row.attributes('tabindex')).toBe('0')
    const bar = row.find<HTMLElement>('.absolute.right-2')
    expect(bar.classes()).toContain('group-focus-within:flex')
    // The root card's reply button too.
    const root = w.find<HTMLElement>('[data-reply-id="root"]')
    expect(root.attributes('tabindex')).toBe('0')
    expect(root.find<HTMLElement>('button.absolute').classes()).toContain('group-focus-within:flex')
  })

  it('arrows move between root and replies', () => {
    setup()
    w.find<HTMLElement>('[data-reply-id="root"]').element.focus()
    key(w.find<HTMLElement>('[data-reply-id="root"]'), { key: 'ArrowDown' })
    expect(requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).dataset.replyId).toBe('r1')
    key(w.find<HTMLElement>('[data-reply-id="r1"]'), { key: 'ArrowDown' })
    expect(requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).dataset.replyId).toBe('r2')
    key(w.find<HTMLElement>('[data-reply-id="r2"]'), { key: 'ArrowUp' })
    expect(requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).dataset.replyId).toBe('r1')
  })

  it('r replies to the focused message, Escape cancels', async () => {
    setup()
    const row = w.find<HTMLElement>('[data-reply-id="r1"]')
    expect(key(row, { key: 'r' }).defaultPrevented).toBe(true)
    await nextTick()
    expect(w.text()).toContain('Antwort an')
    expect(key(row, { key: 'Escape' }).defaultPrevented).toBe(true)
    await nextTick()
    expect(w.text()).not.toContain('Antwort an')
  })

  it('e edits an own reply with focus in the editor; not others\' replies', async () => {
    setup()
    expect(key(w.find<HTMLElement>('[data-reply-id="r1"]'), { key: 'e' }).defaultPrevented).toBe(false)
    const own = w.find<HTMLElement>('[data-reply-id="r2"]')
    own.element.focus()
    key(own, { key: 'e' })
    await flushPromises()
    const editor = w.find<HTMLTextAreaElement>('[data-reply-id="r2"] textarea')
    expect(editor.exists()).toBe(true)
    expect(document.activeElement).toBe(editor.element)
    key(editor, { key: 'Escape' })
    await flushPromises()
    expect(w.find<HTMLTextAreaElement>('[data-reply-id="r2"] textarea').exists()).toBe(false)
    expect(document.activeElement).toBe(w.find<HTMLElement>('[data-reply-id="r2"]').element)
  })
})

describe('ThreadSidebar content', () => {
  it('plays videos in the root and in replies', async () => {
    const chat = setup()
    const video = { id: 'v', url: '/m/v', mime_type: 'video/mp4', is_deleted: false, original_filename: 'clip.mp4', size_bytes: 10 }
    chat.activeThread = msg('root', 'u2', { attachments: [video] })
    chat.threadReplies = [msg('r1', 'u2', { attachments: [{ ...video, id: 'v2' }] })]
    await nextTick()
    expect(w.find<HTMLVideoElement>('[data-reply-id="root"] video').exists()).toBe(true)
    expect(w.find<HTMLVideoElement>('[data-reply-id="r1"] video').exists()).toBe(true)
  })

  it('author names are buttons that open the profile', async () => {
    const chat = setup()
    const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(async () => {})
    const names = w.findAll<HTMLElement>('[data-testid="author-name"]')
    expect(names.length).toBe(4)
    expect(names.every(n => n.element.tagName === 'BUTTON')).toBe(true)
    await requireValue(names[1]).trigger('click')
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: 'r1' }))
  })

  it('clears the file input after a failed upload', async () => {
    const chat = setup()
    vi.spyOn(chat, 'uploadThreadMedia').mockRejectedValue(new Error('nope'))
    const input = w.find<HTMLInputElement>('input[type="file"]').element
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'a.png', { type: 'image/png' })] })
    let value = 'C:\\fakepath\\a.png'
    Object.defineProperty(input, 'value', { configurable: true, get: () => value, set: v => { value = v } })
    input.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(chat.uploadThreadMedia).toHaveBeenCalled()
    expect(value).toBe('')
  })
})

describe('ThreadSidebar replies', () => {
  const textarea = () => w.find<HTMLTextAreaElement>('textarea')

  it('sends with Enter, keeps Shift+Enter for new lines and ignores blank input', async () => {
    const chat = setup()
    const send = vi.spyOn(chat, 'sendThreadReply').mockResolvedValue(null)
    await textarea().setValue('   ')
    key(textarea(), { key: 'Enter' })
    await flushPromises()
    expect(send).not.toHaveBeenCalled()

    await textarea().setValue('  hallo thread  ')
    expect(key(textarea(), { key: 'Enter', shiftKey: true }).defaultPrevented).toBe(false)
    expect(send).not.toHaveBeenCalled()
    expect(key(textarea(), { key: 'Enter' }).defaultPrevented).toBe(true)
    await flushPromises()
    expect(send).toHaveBeenCalledWith('hallo thread', null)
    expect(textarea().element.value).toBe('')
  })

  it('sends as a reply to the chosen message, then clears the target', async () => {
    const chat = setup()
    const send = vi.spyOn(chat, 'sendThreadReply').mockResolvedValue(null)
    key(w.find<HTMLElement>('[data-reply-id="r3"]'), { key: 'r' })
    await nextTick()
    expect(w.text()).toContain('Antwort an')
    await textarea().setValue('genau')
    key(textarea(), { key: 'Enter' })
    await flushPromises()
    expect(send).toHaveBeenCalledWith('genau', 'r3')
    expect(w.text()).not.toContain('Antwort an')
  })

  it('Escape in the composer cancels the reply target', async () => {
    setup()
    await w.find<HTMLElement>('[data-reply-id="root"] button.absolute').trigger('click')
    expect(w.text()).toContain('Antwort an')
    expect(key(textarea(), { key: 'Escape' }).defaultPrevented).toBe(true)
    await nextTick()
    expect(w.text()).not.toContain('Antwort an')
  })

  it('keeps the text and reports a failed send', async () => {
    const chat = setup()
    vi.spyOn(chat, 'sendThreadReply').mockRejectedValueOnce(new Error('')).mockRejectedValueOnce(new Error('zu lang'))
    await textarea().setValue('wichtig')
    key(textarea(), { key: 'Enter' })
    await flushPromises()
    expect(textarea().element.value).toBe('wichtig')
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'error', text: t('thread.sendFailed') })
    key(textarea(), { key: 'Enter' })
    await flushPromises()
    expect(requireValue(useToastStore().toasts.at(-1)).text).toBe('zu lang')
  })

  it('a successful upload clears the reply target', async () => {
    const chat = setup()
    const upload = vi.spyOn(chat, 'uploadThreadMedia').mockResolvedValue(messageFixture())
    key(w.find<HTMLElement>('[data-reply-id="r1"]'), { key: 'r' })
    await nextTick()
    const input = w.find<HTMLInputElement>('input[type="file"]').element
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'a.png', { type: 'image/png' })] })
    input.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(upload).toHaveBeenCalledWith(expect.any(File), '', 'r1')
    expect(w.text()).not.toContain('Antwort an')
  })

  it('jumps to the replied message, or says it is gone', async () => {
    useAuthStore().user = userFixture({ id: 'me', username: 'me', role: 'user' })
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ id: 'ch1', name: 'allgemein', type: 'text' })
    chat.activeThread = msg('root', 'u2')
    chat.threadReplies = [
      msg('r1', 'u2'),
      msg('r2', 'me', { reply_to_id: 'r1', reply_to: { deleted: false, id: 'r1', username: 'u2', display_name: 'U2', content: 'text r1' } }),
      msg('r3', 'me', { reply_to_id: 'gone', reply_to: { id: 'gone', deleted: true } }),
      msg('r4', 'me', { reply_to_id: 'elsewhere', reply_to: { deleted: false, id: 'elsewhere', username: 'x', display_name: 'X', content: 'old' } })
    ]
    w = mount(ThreadSidebar, { attachTo: document.body })
    const target = w.find<HTMLElement>('[data-reply-id="r1"]').element
    target.scrollIntoView = vi.fn()

    await w.find<HTMLElement>('[data-reply-id="r2"] [data-reply-preview] button').trigger('click')
    expect(target.scrollIntoView).toHaveBeenCalledWith({ block: 'center' })

    const notFound = () => useToastStore().toasts.filter(x => x.text === t('chat.messageNotFound')).length
    expect(notFound()).toBe(0)
    await w.find<HTMLElement>('[data-reply-id="r3"] [data-reply-preview] button').trigger('click')
    expect(notFound()).toBe(1)
    await w.find<HTMLElement>('[data-reply-id="r4"] [data-reply-preview] button').trigger('click')
    expect(notFound()).toBe(2)
  })

  it('shows the loading state and closes', async () => {
    const chat = setup()
    chat.threadReplies = []
    chat.isThreadLoading = true
    const close = vi.spyOn(chat, 'closeThread').mockImplementation(async () => {})
    await nextTick()
    expect(w.text()).toContain(t('thread.loading'))
    await w.find<HTMLElement>('button').trigger('click')
    expect(close).toHaveBeenCalled()
  })
})

import ReplyComposerBar from './ReplyComposerBar.vue'
import MessageEditor from './MessageEditor.vue'
import MessageAttachments from './MessageAttachments.vue'
import ReactionBar from './ReactionBar.vue'
import ReactionPalette from './ReactionPalette.vue'
import UserAvatar from './UserAvatar.vue'
import ImageLightbox from './ImageLightbox.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import EmojiButton from './EmojiButton.vue'
import { pendingConfirm } from '../lib/confirm'

const replyComposer = () => requireValue(w.findAll<HTMLTextAreaElement>('textarea').at(-1))
const threadFrame = async () => { await new Promise(resolve => requestAnimationFrame(resolve)); await flushPromises() }

describe('ThreadSidebar complete interactions', () => {
  it('resets reply and editor when thread changes and scrolls appended replies', async () => {
    const chat = setup()
    key(w.find('[data-reply-id=r2]'), { key: 'e' })
    key(w.find('[data-reply-id=r1]'), { key: 'r' })
    await nextTick()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(true)
    chat.activeThread = msg('other-thread', 'u2')
    await flushPromises()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
    expect(w.findComponent(MessageEditor).exists()).toBe(false)
    const container = w.find<HTMLElement>('.overflow-y-auto').element
    Object.defineProperty(container, 'scrollHeight', { configurable: true, value: 800 })
    chat.threadReplies = [...chat.threadReplies, msg('added', 'me')]
    await flushPromises()
    expect(container.scrollTop).toBe(800)
  })

  it('handles empty and missing root/channel fields without stale presentation', async () => {
    const chat = setup()
    chat.activeChannel = null
    chat.activeThread = null
    chat.threadReplies = []
    await nextTick()
    expect(w.find('[data-reply-id=root]').exists()).toBe(false)
    expect(w.text()).toContain(t('thread.empty'))
    chat.activeThread = { id: 'bare', username: 'fallback' }
    chat.threadReplies = [msg('bare-reply', 'me', { display_name: '', content: '', is_edited: true })]
    await nextTick()
    expect(w.find('[data-reply-id=bare]').text()).toContain('fallback')
    expect(w.find('[data-reply-id=bare-reply]').text()).toContain('me')
    expect(w.find('[data-reply-id=bare-reply]').text()).toContain(t('chat.edited'))
  })

  it('guards concurrent sends and uploads while keeping the draft editable', async () => {
    const chat = setup()
    let sent!: (message: null) => void
    const send = vi.spyOn(chat, 'sendThreadReply').mockImplementation(() => new Promise<null>(resolve => { sent = resolve }))
    await replyComposer().setValue('hello')
    key(replyComposer(), { key: 'Enter' })
    key(replyComposer(), { key: 'Enter' })
    expect(send).toHaveBeenCalledTimes(1)
    sent(null)
    await flushPromises()
    const input = w.find<HTMLInputElement>('input[type=file]').element
    const picker = vi.spyOn(input, 'click').mockImplementation(() => {})
    const uploadButton = requireValue(w.findAll('button').find(button => button.find('svg.lucide-plus').exists()))
    await uploadButton.trigger('click')
    expect(picker).toHaveBeenCalledTimes(1)
    let uploaded!: (message: ReturnType<typeof messageFixture>) => void
    vi.spyOn(chat, 'uploadThreadMedia').mockImplementation(() => new Promise(resolve => { uploaded = resolve }))
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'image.png', { type: 'image/png' })] })
    input.dispatchEvent(new Event('change'))
    await replyComposer().setValue('keep')
    key(replyComposer(), { key: 'Enter' })
    expect(send).toHaveBeenCalledTimes(1)
    uploaded(messageFixture())
    await flushPromises()
    expect(replyComposer().element.value).toBe('keep')
  })

  it('cancels replies through the bar and row Escape, and cancels edits through root/reply Escape', async () => {
    setup()
    const root = w.find('[data-reply-id=root]')
    expect(key(root, { key: 'Escape' }).defaultPrevented).toBe(false)
    key(root, { key: 'r' })
    await nextTick()
    w.findComponent(ReplyComposerBar).vm.$emit('cancel')
    await nextTick()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
    key(root, { key: 'r' })
    await nextTick()
    key(root, { key: 'Escape' })
    await nextTick()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
    key(w.find('[data-reply-id=r2]'), { key: 'e' })
    await nextTick()
    key(root, { key: 'Escape' })
    await nextTick()
    expect(w.findComponent(MessageEditor).exists()).toBe(false)
    key(w.find('[data-reply-id=r2]'), { key: 'e' })
    await nextTick()
    key(w.find('[data-reply-id=r1]'), { key: 'Escape' })
    await nextTick()
    expect(w.findComponent(MessageEditor).exists()).toBe(false)
    expect(key(w.find('[data-reply-id=r1]'), { key: 'Escape' }).defaultPrevented).toBe(false)
  })

  it('edits via the quick action, binds the editor, saves and cancels via child events', async () => {
    const chat = setup()
    const edit = vi.spyOn(chat, 'editMessage').mockResolvedValue(null)
    const own = w.find('[data-reply-id=r2]')
    await requireValue(own.findAll('button').find(button => button.find('svg.lucide-pencil').exists())).trigger('click')
    let editor = w.findComponent(MessageEditor)
    editor.vm.$emit('update:modelValue', 'modified')
    editor.vm.$emit('save')
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('ch1', 'r2', 'modified')
    await requireValue(own.findAll('button').find(button => button.find('svg.lucide-pencil').exists())).trigger('click')
    editor = w.findComponent(MessageEditor)
    editor.vm.$emit('cancel')
    await nextTick()
    expect(w.findComponent(MessageEditor).exists()).toBe(false)
  })

  it('opens profile avatars, attachment lightboxes and root/reply reaction palettes', async () => {
    const chat = setup()
    const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(undefined)
    const react = vi.spyOn(chat, 'toggleReaction').mockResolvedValue([])
    for (const avatar of w.findAllComponents(UserAvatar)) avatar.vm.$emit('click')
    expect(profile).toHaveBeenCalledTimes(4)
    expect(profile).toHaveBeenCalledWith(expect.objectContaining({ id: 'root' }))
    await w.find('[data-reply-id=root] [data-testid=author-name]').trigger('click')
    expect(profile).toHaveBeenCalledTimes(5)
    for (const attachment of w.findAllComponents(MessageAttachments)) {
      attachment.vm.$emit('open-image', '/test-image')
      await nextTick()
      expect(w.findComponent(ImageLightbox).props('src')).toBe('/test-image')
      w.findComponent(ImageLightbox).vm.$emit('close')
      await nextTick()
    }
    for (const bar of w.findAllComponents(ReactionBar)) {
      bar.vm.$emit('toggle', '👍')
      bar.vm.$emit('toggle-picker')
      await nextTick()
      expect(bar.props('pickerOpen')).toBe(true)
      bar.vm.$emit('close-picker')
      await nextTick()
      expect(bar.props('pickerOpen')).toBe(false)
    }
    expect(react).toHaveBeenCalledTimes(4)
    const reply = w.find('[data-reply-id=r1]')
    const quickPicker = reply.find('.reaction-picker-anchor button')
    await quickPicker.trigger('click')
    expect(w.findComponent(ReactionPalette).exists()).toBe(true)
    w.findComponent(ReactionPalette).vm.$emit('pick', '🙂')
    await flushPromises()
    expect(react).toHaveBeenCalledWith('r1', '🙂')
    await quickPicker.trigger('click')
    w.findComponent(ReactionPalette).vm.$emit('close')
    await nextTick()
    expect(w.findComponent(ReactionPalette).exists()).toBe(false)
    await requireValue(reply.findAll('button').find(button => button.find('svg.lucide-reply').exists())).trigger('click')
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(true)
  })

  it('delete quick action opens the confirmation and dispatches deletion', async () => {
    const chat = setup()
    const del = vi.spyOn(chat, 'deleteMessage').mockResolvedValue(undefined)
    await requireValue(w.find('[data-reply-id=r2]').findAll('button').find(button => button.find('svg.lucide-trash-2').exists())).trigger('click')
    await nextTick()
    expect(pendingConfirm.value).toMatchObject({ title: t('thread.deleteTitle'), danger: true })
    requireValue(pendingConfirm.value).resolve(true)
    await flushPromises()
    expect(del).toHaveBeenCalledWith('ch1', 'r2')
  })

  it('selects mention suggestions with keyboard and mouse, then inserts emoji', async () => {
    const chat = setup()
    chat.members = [userFixture({ username: 'anna', display_name: 'Anna' })]
    const send = vi.spyOn(chat, 'sendThreadReply').mockResolvedValue(null)
    await replyComposer().setValue('@a')
    replyComposer().element.selectionStart = 2
    await replyComposer().trigger('click')
    const list = w.findComponent(MentionSuggestions)
    expect(list.exists()).toBe(true)
    list.vm.$emit('hover', 0)
    key(replyComposer(), { key: 'Enter' })
    await nextTick()
    expect(send).not.toHaveBeenCalled()
    expect(replyComposer().element.value).toBe('@anna ')
    await replyComposer().setValue('@a')
    replyComposer().element.selectionStart = 2
    await replyComposer().trigger('keyup', { key: 'ArrowLeft' })
    await replyComposer().trigger('keyup', { key: 'ArrowRight' })
    w.findComponent(MentionSuggestions).vm.$emit('pick', requireValue(w.findComponent(MentionSuggestions).props('items')[0]))
    await nextTick()
    w.findComponent(EmojiButton).vm.$emit('pick', '🙂')
    await replyComposer().trigger('blur')
    expect(replyComposer().element.value).toContain('🙂')
  })

  it('flashes reply and root navigation targets briefly, and rejects missing reference IDs', async () => {
    const chat = setup()
    chat.threadReplies = [msg('r1', 'u2', { reply_to_id: 'root', reply_to: { id: 'root', deleted: false, content: 'root', username: 'u2' } })]
    await nextTick()
    const root = w.find<HTMLElement>('[data-reply-id=root]').element
    root.scrollIntoView = vi.fn()
    await w.find('[data-reply-preview] button').trigger('click')
    await threadFrame()
    expect(root.scrollIntoView).toHaveBeenCalledWith({ block: 'center' })
    expect(w.find('[data-reply-id=root]').classes()).toContain('msg-flash')
    await new Promise(resolve => setTimeout(resolve, 2050))
    await nextTick()
    expect(w.find('[data-reply-id=root]').classes()).not.toContain('msg-flash')
    chat.threadReplies = [msg('r1', 'u2', { reply_to_id: 'r2', reply_to: { id: 'r2', deleted: false, content: 'reply', username: 'u2' } }), msg('r2', 'me')]
    await nextTick()
    w.find<HTMLElement>('[data-reply-id=r2]').element.scrollIntoView = vi.fn()
    await w.find('[data-reply-preview] button').trigger('click')
    await threadFrame()
    expect(w.find('[data-reply-id=r2]').classes()).toContain('msg-flash')
    chat.threadReplies = [msg('bad', 'u2', { reply_to: { id: 'missing', deleted: false, username: 'u2', content: 'bad' } })]
    await nextTick()
    await w.find('[data-reply-preview] button').trigger('click')
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('chat.messageNotFound'))
  })
})
