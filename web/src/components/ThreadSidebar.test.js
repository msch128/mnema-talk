import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import ThreadSidebar from './ThreadSidebar.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

let w

function msg(id, user, extra = {}) {
  return {
    id, channel_id: 'ch1', user_id: user, username: user, display_name: user.toUpperCase(),
    content: 'text ' + id, created_at: '2026-01-01T10:00:00Z', attachments: [], reactions: [], ...extra
  }
}

function setup() {
  useAuthStore().user = { id: 'me', username: 'me', role: 'user' }
  const chat = useChatStore()
  chat.activeChannel = { id: 'ch1', name: 'allgemein', type: 'text' }
  chat.activeThread = msg('root', 'u2')
  chat.threadReplies = [msg('r1', 'u2'), msg('r2', 'me'), msg('r3', 'u2')]
  w = mount(ThreadSidebar, { attachTo: document.body })
  return chat
}

const key = (el, init) => {
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
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ThreadSidebar keyboard', () => {
  it('replies are focusable and show their actions on focus', () => {
    setup()
    const row = w.find('[data-reply-id="r1"]')
    expect(row.attributes('tabindex')).toBe('0')
    const bar = row.find('.absolute.right-2')
    expect(bar.classes()).toContain('group-focus-within:flex')
    // The root card's reply button too.
    const root = w.find('[data-reply-id="root"]')
    expect(root.attributes('tabindex')).toBe('0')
    expect(root.find('button.absolute').classes()).toContain('group-focus-within:flex')
  })

  it('arrows move between root and replies', () => {
    setup()
    w.find('[data-reply-id="root"]').element.focus()
    key(w.find('[data-reply-id="root"]'), { key: 'ArrowDown' })
    expect(document.activeElement.dataset.replyId).toBe('r1')
    key(w.find('[data-reply-id="r1"]'), { key: 'ArrowDown' })
    expect(document.activeElement.dataset.replyId).toBe('r2')
    key(w.find('[data-reply-id="r2"]'), { key: 'ArrowUp' })
    expect(document.activeElement.dataset.replyId).toBe('r1')
  })

  it('r replies to the focused message, Escape cancels', async () => {
    setup()
    const row = w.find('[data-reply-id="r1"]')
    expect(key(row, { key: 'r' }).defaultPrevented).toBe(true)
    await nextTick()
    expect(w.text()).toContain('Antwort an')
    expect(key(row, { key: 'Escape' }).defaultPrevented).toBe(true)
    await nextTick()
    expect(w.text()).not.toContain('Antwort an')
  })

  it('e edits an own reply with focus in the editor; not others\' replies', async () => {
    setup()
    expect(key(w.find('[data-reply-id="r1"]'), { key: 'e' }).defaultPrevented).toBe(false)
    const own = w.find('[data-reply-id="r2"]')
    own.element.focus()
    key(own, { key: 'e' })
    await flushPromises()
    const editor = w.find('[data-reply-id="r2"] textarea')
    expect(editor.exists()).toBe(true)
    expect(document.activeElement).toBe(editor.element)
    key(editor, { key: 'Escape' })
    await flushPromises()
    expect(w.find('[data-reply-id="r2"] textarea').exists()).toBe(false)
    expect(document.activeElement).toBe(w.find('[data-reply-id="r2"]').element)
  })
})

describe('ThreadSidebar content', () => {
  it('plays videos in the root and in replies', async () => {
    const chat = setup()
    const video = { id: 'v', url: '/m/v', mime_type: 'video/mp4', original_filename: 'clip.mp4', size_bytes: 10 }
    chat.activeThread = msg('root', 'u2', { attachments: [video] })
    chat.threadReplies = [msg('r1', 'u2', { attachments: [{ ...video, id: 'v2' }] })]
    await nextTick()
    expect(w.find('[data-reply-id="root"] video').exists()).toBe(true)
    expect(w.find('[data-reply-id="r1"] video').exists()).toBe(true)
  })

  it('author names are buttons that open the profile', async () => {
    const chat = setup()
    const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(() => {})
    const names = w.findAll('[data-testid="author-name"]')
    expect(names.length).toBe(4)
    expect(names.every(n => n.element.tagName === 'BUTTON')).toBe(true)
    await names[1].trigger('click')
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: 'r1' }))
  })

  it('clears the file input after a failed upload', async () => {
    const chat = setup()
    vi.spyOn(chat, 'uploadThreadMedia').mockRejectedValue(new Error('nope'))
    const input = w.find('input[type="file"]').element
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'a.png', { type: 'image/png' })] })
    let value = 'C:\\fakepath\\a.png'
    Object.defineProperty(input, 'value', { configurable: true, get: () => value, set: v => { value = v } })
    input.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(chat.uploadThreadMedia).toHaveBeenCalled()
    expect(value).toBe('')
  })
})
