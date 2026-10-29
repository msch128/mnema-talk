import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import ChatArea from './ChatArea.vue'
import ThreadSidebar from './ThreadSidebar.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useOnboardingStore } from '../stores/onboarding'
import { pendingConfirm } from '../lib/confirm'
import { channelFixture, messageFixture, onboardingStateFixture, userFixture } from '../test-fixtures.fixture'
import type { Message } from '../types/domain'

enableAutoUnmount(afterEach)

const general = channelFixture({ id: 'ch1', name: 'lobby', type: 'text' })

function msg(id: string, user: string, extra: Partial<Message> = {}) {
  return messageFixture({
    id, channel_id: 'ch1', user_id: user, username: user, content: `text ${id}`,
    created_at: '2026-01-01T10:00:00Z', attachments: [], reactions: [], ...extra
  })
}

function open(props: { panel?: boolean } = {}) {
  return mount(ChatArea, { attachTo: document.body, props })
}
type View = ReturnType<typeof open>

const composer = (view: View) => view.get<HTMLTextAreaElement>('[data-composer-input]')
const editor = (view: View) => view.find<HTMLTextAreaElement>('[data-testid="message-editor"]')
const editingId = (view: View) => editor(view).element.closest('[data-msg-id]')?.getAttribute('data-msg-id')

async function up(el: { trigger: (e: string, o: object) => Promise<unknown> }) {
  await el.trigger('keydown', { key: 'ArrowUp' })
  await nextTick()
}
async function down(el: { trigger: (e: string, o: object) => Promise<unknown> }) {
  await el.trigger('keydown', { key: 'ArrowDown' })
  await nextTick()
}

function caretAt(el: HTMLTextAreaElement, pos: number) {
  el.focus()
  el.setSelectionRange(pos, pos)
}

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me', role: 'user' })
  useOnboardingStore().state = onboardingStateFixture({ dismissed_tips: ['search', 'mention', 'talk'] })
  const chat = useChatStore()
  chat.activeChannel = general
  chat.messages = [
    msg('m1', 'me'), msg('m2', 'u2'), msg('m3', 'me'), msg('m4', 'me', { poll: { id: 'p', question: 'q', options: [] } as never }),
    msg('m5', 'u2'), msg('m6', 'me', { content: '' }), msg('m7', 'me', { forward: { channel_id: 'x' } as never })
  ]
  vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('ArrowUp in the composer', () => {
  it('opens the newest own message that can be edited, with the caret at its end', async () => {
    const view = open()
    await up(composer(view))
    await flushPromises()
    expect(editingId(view)).toBe('m3') // skips the poll, the empty one, the forward and other people's
    expect(editor(view).element.value).toBe('text m3')
    expect(editor(view).element.selectionStart).toBe('text m3'.length)
    expect(document.activeElement).toBe(editor(view).element)
    expect(editor(view).attributes('aria-describedby')).toBeTruthy()
  })

  it('leaves the key alone when the composer has text', async () => {
    const view = open()
    await composer(view).setValue('draft')
    await up(composer(view))
    expect(editor(view).exists()).toBe(false)
  })

  it('leaves the key alone with a modifier, and when there is nothing of your own', async () => {
    const view = open()
    await composer(view).trigger('keydown', { key: 'ArrowUp', shiftKey: true })
    expect(editor(view).exists()).toBe(false)
    useChatStore().messages = [msg('x', 'u2')]
    await nextTick()
    await up(composer(view))
    expect(editor(view).exists()).toBe(false)
  })

  it('does not take the key while the mention list is open', async () => {
    useChatStore().members = [userFixture({ id: 'u2', username: 'anna', display_name: 'Anna' }), userFixture({ id: 'u3', username: 'anton', display_name: 'Anton' })]
    const view = open()
    await composer(view).setValue('@an')
    caretAt(composer(view).element, 3)
    await composer(view).trigger('input')
    await up(composer(view))
    expect(editor(view).exists()).toBe(false)
  })

  it('works in the Talk chat panel', async () => {
    const view = open({ panel: true })
    await up(composer(view))
    expect(editingId(view)).toBe('m3')
  })
})

describe('walking through own messages', () => {
  it('goes older on ArrowUp, newer on ArrowDown, and back to the composer past the newest', async () => {
    const view = open()
    await up(composer(view))
    await up(editor(view))
    expect(editingId(view)).toBe('m1')
    await up(editor(view)) // nothing older: stays
    expect(editingId(view)).toBe('m1')
    await down(editor(view))
    expect(editingId(view)).toBe('m3')
    await down(editor(view))
    expect(editor(view).exists()).toBe(false)
    expect(document.activeElement).toBe(composer(view).element)
  })

  it('moves only from the first or last line of a multi-line text', async () => {
    useChatStore().messages = [msg('a', 'me'), msg('b', 'me', { content: 'one\ntwo\nthree' })]
    const view = open()
    await up(composer(view))
    const box = editor(view).element
    caretAt(box, 5) // second line
    await up(editor(view))
    expect(editingId(view)).toBe('b')
    caretAt(box, 1) // first line
    await down(editor(view))
    expect(editingId(view)).toBe('b')
    caretAt(box, box.value.length) // last line
    await down(editor(view))
    expect(editor(view).exists()).toBe(false)
  })

  it('keeps a selection and unsaved changes where they are', async () => {
    const view = open()
    await up(composer(view))
    await up(editor(view))
    await editor(view).setValue('changed')
    await up(editor(view))
    expect(editingId(view)).toBe('m1')
    expect(editor(view).element.value).toBe('changed')
  })
})

describe('saving and cancelling', () => {
  it('Enter saves, Shift+Enter does not, and the composer gets the focus back', async () => {
    const chat = useChatStore()
    const edit = vi.spyOn(chat, 'editMessage').mockResolvedValue(undefined as never)
    const view = open()
    await up(composer(view))
    await editor(view).setValue('new text')
    await editor(view).trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(edit).not.toHaveBeenCalled()
    await editor(view).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('ch1', 'm3', 'new text')
    expect(editor(view).exists()).toBe(false)
    expect(document.activeElement).toBe(composer(view).element)
  })

  it('Escape cancels and returns to the composer', async () => {
    const view = open()
    await up(composer(view))
    await editor(view).trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(editor(view).exists()).toBe(false)
    expect(document.activeElement).toBe(composer(view).element)
  })

  it('saving an empty text asks to delete the message', async () => {
    const chat = useChatStore()
    const del = vi.spyOn(chat, 'deleteMessage').mockResolvedValue(undefined as never)
    const edit = vi.spyOn(chat, 'editMessage')
    const view = open()
    await up(composer(view))
    await editor(view).setValue('  ')
    await editor(view).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(pendingConfirm.value?.danger).toBe(true)
    pendingConfirm.value?.resolve(false) // declined: the editor stays
    await flushPromises()
    expect(del).not.toHaveBeenCalled()
    expect(editor(view).exists()).toBe(true)

    await editor(view).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(del).toHaveBeenCalledWith('ch1', 'm3')
    expect(edit).not.toHaveBeenCalled()
    expect(editor(view).exists()).toBe(false)
  })
})

describe('in a thread', () => {
  it('ArrowUp in the reply box edits the newest own reply and walks back', async () => {
    const chat = useChatStore()
    chat.activeThread = msg('root', 'u2')
    chat.threadReplies = [msg('r1', 'me'), msg('r2', 'u2'), msg('r3', 'me')]
    const view = mount(ThreadSidebar, { attachTo: document.body })
    await up(view.get('textarea'))
    const id = () => view.get('[data-testid="message-editor"]').element.closest('[data-reply-id]')?.getAttribute('data-reply-id')
    expect(id()).toBe('r3')
    await up(view.get('[data-testid="message-editor"]'))
    expect(id()).toBe('r1')
    await down(view.get('[data-testid="message-editor"]'))
    await down(view.get('[data-testid="message-editor"]'))
    expect(view.find('[data-testid="message-editor"]').exists()).toBe(false)
  })
})
