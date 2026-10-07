import { required } from '../store-test-support.fixture'
import { useMessageActions } from './useMessageActions'
import type { ContextMenuItem } from '../components/menuTypes'
import { channelFixture, messageFixture } from '../test-fixtures.fixture'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageMenu } from './useMessageMenu'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const msg = messageFixture({ id: 'm1', content: 'hello world', user_id: 'me' })

function makeActions({ own = true, canDelete = true } = {}) {
  return {
    ...useMessageActions(),
    pickerId: ref<string | null>(null),
    isOwn: vi.fn(() => own),
    canDelete: vi.fn(() => canDelete),
    startEdit: vi.fn(),
    deleteMessage: vi.fn(async () => {}),
    closePicker: vi.fn()
  }
}

let chat: ReturnType<typeof useChatStore>
const labels = (items: ContextMenuItem[]) => items.map(i => i.type === 'separator' ? '—' : i.label)
const item = (items: ContextMenuItem[], label: string) => {
  const result = required(items.find(i => i.label === label))
  return { ...result, action: required(result.action) }
}
const toasts = () => useToastStore().toasts

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useMessageMenu items', () => {
  it('uses the message channel when no channel is active and handles non-Error failures', async () => {
    chat.activeChannel = null
    chat.markChannelUnread = vi.fn().mockRejectedValue('offline')
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    await item(menu.itemsFor(msg), 'Mark unread').action()
    expect(chat.markChannelUnread).toHaveBeenCalledWith(msg.channel_id, msg.id)
    expect(toasts().at(-1)).toMatchObject({ type: 'error', text: 'Could not mark as unread.' })
  })
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
    chat.openThread = vi.fn(async () => {})
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
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const items = useMessageMenu({ actions: makeActions(), reply: vi.fn() }).itemsFor(msg)
    await item(items, 'Copy text').action()
    expect(writeText).toHaveBeenLastCalledWith('hello world')
    expect(required(toasts().at(-1)).text).toBe('Text copied')
    await item(items, 'Copy link').action()
    expect(writeText).toHaveBeenLastCalledWith(`${window.location.origin}/c/ch1/m/m1`)
    expect(required(toasts().at(-1)).text).toBe('Link copied')
  })

  it('copies an empty string for attachment-only messages and tolerates a blocked clipboard', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const items = useMessageMenu({ actions: makeActions(), reply: vi.fn() }).itemsFor(messageFixture({ id: 'm2', content: '' }))
    await item(items, 'Copy text').action()
    expect(writeText).toHaveBeenCalledWith('')
    expect(toasts()).toHaveLength(0)
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('marks the channel unread from the message and reports failures', async () => {
    chat.markChannelUnread = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('')).mockRejectedValueOnce(new Error('offline'))
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
  it('ignores a detached button event and uses zero coordinates without an element', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.openFromButton(new MouseEvent('click'), msg)
    expect(menu.state.value.open).toBe(false)
    menu.onContextMenu(new MouseEvent('contextmenu'), msg)
    expect(menu.state.value).toMatchObject({ open: true, x: 0, y: 0 })
  })
  const rect = (r: Partial<Pick<DOMRect, 'left' | 'top' | 'bottom'>>) => {
    const element = document.createElement('div')
    element.getBoundingClientRect = () => new DOMRect(r.left ?? 0, r.top ?? 0, 0, (r.bottom ?? r.top ?? 0) - (r.top ?? 0))
    return element
  }

  const trigger = (init: MouseEventInit, element: Element) => {
    const event = new MouseEvent('contextmenu', init)
    Object.defineProperty(event, 'currentTarget', { value: element })
    return event
  }

  it('opens at the pointer on right-click and closes the reaction picker', () => {
    const actions = makeActions()
    const menu = useMessageMenu({ actions, reply: vi.fn() })
    menu.onContextMenu(trigger({ clientX: 120, clientY: 340 }, rect({})), msg)
    expect(menu.state.value).toMatchObject({ open: true, x: 120, y: 340 })
    expect(menu.state.value.items.length).toBeGreaterThan(0)
    expect(actions.closePicker).toHaveBeenCalled()
    menu.close()
    expect(menu.state.value.open).toBe(false)
  })

  it('opens at the message for the menu key (no pointer position)', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.onContextMenu(trigger({ clientX: 0, clientY: 0 }, rect({ left: 10, top: 200 })), msg)
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
    menu.openFromButton(trigger({}, rect({ left: 300, bottom: 50 })), msg)
    expect(menu.state.value).toMatchObject({ open: true, x: 300, y: 54 })
  })

  it('show() defaults a missing position to the corner', () => {
    const menu = useMessageMenu({ actions: makeActions(), reply: vi.fn() })
    menu.show(0, 0, [{ label: 'x' }])
    expect(menu.state.value).toEqual({ open: true, x: 0, y: 0, items: [{ label: 'x' }] })
  })
})
