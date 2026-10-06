import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageMenu } from './useMessageMenu'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const msg = { id: 'm1', content: 'hello world', user_id: 'me' }

function makeActions({ own = true, canDelete = true } = {}) {
  return {
    pickerId: ref(null),
    isOwn: vi.fn(() => own),
    canDelete: vi.fn(() => canDelete),
    startEdit: vi.fn(),
    deleteMessage: vi.fn(),
    closePicker: vi.fn()
  }
}

let chat
const labels = items => items.map(i => i.type === 'separator' ? '—' : i.label)
const item = (items, label) => items.find(i => i.label === label)
const toasts = () => useToastStore().toasts

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  chat = useChatStore()
  chat.activeChannel = { id: 'ch1', name: 'general', type: 'text' }
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useMessageMenu items', () => {
  it('offers edit and delete on own messages', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    expect(labels(menu.itemsFor(msg))).toEqual([
      'Reply', 'Add reaction', 'Open thread', 'Edit message', '—',
      'Copy text', 'Copy link', 'Mark unread', '—', 'Delete message'
    ])
  })

  it('hides edit and delete on messages the user may not change', () => {
    const menu = useMessageMenu({ actions: makeActions({ own: false, canDelete: false }), reply: vi.fn() })
    const l = labels(menu.itemsFor(msg))
    expect(l).not.toContain('Edit message')
    expect(l).not.toContain('Delete message')
    expect(l.at(-1)).toBe('Mark unread')
  })

  it('wires each item to its action', () => {
    const actions = makeActions()
    const reply = vi.fn()
    chat.openThread = vi.fn()
    const items = useMessageMenu({ actions, reply }).itemsFor(msg)
    item(items, 'Reply').action()
    expect(reply).toHaveBeenCalledWith(msg)
    item(items, 'Add reaction').action()
    expect(actions.pickerId.value).toBe('m1')
    item(items, 'Open thread').action()
    expect(chat.openThread).toHaveBeenCalledWith(msg)
    item(items, 'Edit message').action()
    expect(actions.startEdit).toHaveBeenCalledWith(msg)
    const del = item(items, 'Delete message')
    expect(del.danger).toBe(true)
    del.action()
    expect(actions.deleteMessage).toHaveBeenCalledWith(msg)
  })

  it('copies the text and a link to the message', async () => {
    const writeText = vi.fn().mockResolvedValue()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const items = useMessageMenu({ actions: makeActions(), reply: vi.fn() }).itemsFor(msg)
    await item(items, 'Copy text').action()
    expect(writeText).toHaveBeenLastCalledWith('hello world')
    expect(toasts().at(-1).text).toBe('Text copied')
    await item(items, 'Copy link').action()
    expect(writeText).toHaveBeenLastCalledWith(`${window.location.origin}/c/ch1/m/m1`)
    expect(toasts().at(-1).text).toBe('Link copied')
  })

  it('copies an empty string for attachment-only messages and tolerates a blocked clipboard', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const items = useMessageMenu({ actions: makeActions(), reply: vi.fn() }).itemsFor({ id: 'm2', content: null })
    await item(items, 'Copy text').action()
    expect(writeText).toHaveBeenCalledWith('')
    expect(toasts()).toHaveLength(0)
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('marks the channel unread from the message and reports failures', async () => {
    chat.markChannelUnread = vi.fn().mockResolvedValueOnce().mockRejectedValueOnce(new Error('')).mockRejectedValueOnce(new Error('offline'))
    const items = useMessageMenu({ actions: makeActions(), reply: vi.fn() }).itemsFor(msg)
    const mark = item(items, 'Mark unread')
    await mark.action()
    expect(chat.markChannelUnread).toHaveBeenCalledWith('ch1', 'm1')
    expect(toasts().at(-1)).toMatchObject({ type: 'success', text: 'Marked as unread' })
    await mark.action()
    expect(toasts().at(-1)).toMatchObject({ type: 'error', text: 'Could not mark as unread.' })
    await mark.action()
    expect(toasts().at(-1)).toMatchObject({ type: 'error', text: 'offline' })
  })
})

describe('useMessageMenu positioning', () => {
  const rect = r => ({ getBoundingClientRect: () => ({ left: 0, top: 0, bottom: 0, ...r }) })

  it('opens at the pointer on right-click and closes the reaction picker', () => {
    const actions = makeActions()
    const menu = useMessageMenu({ actions, reply: vi.fn() })
    menu.onContextMenu({ clientX: 120, clientY: 340, currentTarget: rect({}) }, msg)
    expect(menu.state.value).toMatchObject({ open: true, x: 120, y: 340 })
    expect(menu.state.value.items.length).toBeGreaterThan(0)
    expect(actions.closePicker).toHaveBeenCalled()
    menu.close()
    expect(menu.state.value.open).toBe(false)
  })

  it('opens at the message for the menu key (no pointer position)', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.onContextMenu({ clientX: 0, clientY: 0, currentTarget: rect({ left: 10, top: 200 }) }, msg)
    expect(menu.state.value).toMatchObject({ open: true, x: 90, y: 224 })
  })

  it('keeps the keyboard menu on screen for messages scrolled out of view', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.openAtElement(rect({ left: 0, top: -500 }), msg)
    expect(menu.state.value.y).toBe(24)
    menu.openAtElement(rect({ left: 0, top: window.innerHeight + 500 }), msg)
    expect(menu.state.value.y).toBe(window.innerHeight - 24)
  })

  it('opens below the ⋯ button', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.openFromButton({ currentTarget: rect({ left: 300, bottom: 50 }) }, msg)
    expect(menu.state.value).toMatchObject({ open: true, x: 300, y: 54 })
  })

  it('show() defaults a missing position to the corner', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.show(undefined, null, [{ label: 'x' }])
    expect(menu.state.value).toEqual({ open: true, x: 0, y: 0, items: [{ label: 'x' }] })
  })
})
