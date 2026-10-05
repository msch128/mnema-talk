import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import ChatArea from './ChatArea.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale, i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

let w
let calls

function msg(id, user, min, content = 'text ' + id) {
  return {
    id, channel_id: 'ch1', user_id: user, username: user, display_name: user.toUpperCase(),
    content, created_at: new Date(Date.UTC(2026, 0, 1, 10, min)).toISOString(), attachments: [], reactions: []
  }
}

function setup({ lastReadAt = null, notifPermission = 'granted' } = {}) {
  const auth = useAuthStore()
  auth.user = { id: 'me', username: 'me', role: 'user' }
  const chat = useChatStore()
  chat.notificationPermission = notifPermission
  chat.categories = [{ id: 'c', channels: [{ id: 'ch1', name: 'allgemein', type: 'text' }] }]
  chat.activeChannel = { id: 'ch1', name: 'allgemein', type: 'text' }
  chat.activeChannelLastReadAt = lastReadAt
  chat.members = [{ id: 'u2', username: 'anna', display_name: 'Anna' }]
  chat.messages = [msg('a', 'u2', 0), msg('b', 'u2', 30), msg('c', 'me', 40), msg('d', 'u2', 50)]
  w = mount(ChatArea, {
    attachTo: document.body,
    global: { plugins: [i18nPlugin], directives: { tooltip } }
  })
  return chat
}

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  calls = []
  vi.stubGlobal('fetch', vi.fn(async (url, init) => {
    calls.push({ url, method: init?.method || 'GET' })
    return new Response(null, { status: 204 })
  }))
})

afterEach(() => {
  w?.unmount()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const key = (el, init) => el.element.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }))

describe('ChatArea new-messages divider', () => {
  it('renders "Neu seit" before the first unread message of someone else', async () => {
    setup({ lastReadAt: new Date(Date.UTC(2026, 0, 1, 10, 35)).toISOString() })
    await nextTick()
    const sep = w.findAll('[role="separator"]')
    expect(sep).toHaveLength(1)
    expect(sep[0].text()).toMatch(/^Neu seit/)
    // Placed directly before message "d" (own message "c" is not "new").
    expect(sep[0].element.nextElementSibling.dataset.msgId).toBe('d')
  })

  it('shows no divider without a last-read time', async () => {
    setup()
    await nextTick()
    expect(w.find('[role="separator"]').exists()).toBe(false)
  })

  it('Esc marks the channel read and removes the divider', async () => {
    setup({ lastReadAt: new Date(Date.UTC(2026, 0, 1, 10, 5)).toISOString() })
    await nextTick()
    expect(w.find('[role="separator"]').exists()).toBe(true)
    key(w.find('textarea'), { key: 'Escape' })
    await flushPromises()
    expect(calls.some(c => c.method === 'POST' && c.url === '/api/channels/ch1/read')).toBe(true)
    expect(w.find('[role="separator"]').exists()).toBe(false)
  })

  it('Esc does nothing while the composer has text', async () => {
    setup({ lastReadAt: new Date(Date.UTC(2026, 0, 1, 10, 5)).toISOString() })
    await w.find('textarea').setValue('halb fertig')
    calls.length = 0
    key(w.find('textarea'), { key: 'Escape' })
    await flushPromises()
    expect(calls.filter(c => c.url.endsWith('/read'))).toHaveLength(0)
    expect(w.find('[role="separator"]').exists()).toBe(true)
  })
})

describe('ChatArea typing indicator', () => {
  it('names one, two and several typers', async () => {
    const chat = setup()
    chat.typingByChannel = { ch1: [{ user_id: 'u2', display_name: 'Anna' }] }
    await nextTick()
    expect(w.find('[role="status"]').text()).toBe('Anna tippt …')
    chat.typingByChannel = { ch1: [{ user_id: 'u2', display_name: 'Anna' }, { user_id: 'u3', username: 'ben' }] }
    await nextTick()
    expect(w.find('[role="status"]').text()).toBe('Anna und ben tippen …')
    chat.typingByChannel = { ch1: [{ user_id: 'u2', username: 'a' }, { user_id: 'u3', username: 'b' }, { user_id: 'u4', username: 'c' }] }
    await nextTick()
    expect(w.find('[role="status"]').text()).toBe('Mehrere Personen tippen …')
  })

  it('announces typing through the composer, but not for empty text', async () => {
    const chat = setup()
    const spy = vi.spyOn(chat, 'sendTyping')
    await w.find('textarea').setValue('   ')
    expect(spy).not.toHaveBeenCalled()
    await w.find('textarea').setValue('hallo')
    expect(spy).toHaveBeenCalledWith('ch1')
  })
})

describe('ChatArea notification opt-in', () => {
  it('asks only after a click on the enable button', async () => {
    const N = { permission: 'default', requestPermission: vi.fn(async () => 'granted') }
    vi.stubGlobal('Notification', N)
    setup({ notifPermission: 'default' })
    expect(N.requestPermission).not.toHaveBeenCalled()
    const btn = w.findAll('button').find(b => b.text() === 'Aktivieren')
    expect(btn).toBeTruthy()
    await btn.trigger('click')
    await flushPromises()
    expect(N.requestPermission).toHaveBeenCalledTimes(1)
    expect(w.findAll('button').some(b => b.text() === 'Aktivieren')).toBe(false)
  })

  it('stays hidden when permission is already decided', () => {
    setup({ notifPermission: 'granted' })
    expect(w.findAll('button').some(b => b.text() === 'Aktivieren')).toBe(false)
  })
})

describe('ChatArea message keyboard handling', () => {
  it('r starts a reply but Ctrl/Cmd+R is left to the browser', async () => {
    setup()
    const row = w.find('[data-msg-id="a"]')
    key(row, { key: 'r', ctrlKey: true })
    key(row, { key: 'r', metaKey: true })
    await nextTick()
    expect(w.text()).not.toContain('Antwort an')
    const ev = new KeyboardEvent('keydown', { key: 'r', bubbles: true, cancelable: true })
    row.element.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
    await nextTick()
    expect(w.text()).toContain('Antwort an')
  })

  it('ArrowDown / ArrowUp move focus between messages', async () => {
    setup()
    w.find('[data-msg-id="a"]').element.focus()
    key(w.find('[data-msg-id="a"]'), { key: 'ArrowDown' })
    expect(document.activeElement.dataset.msgId).toBe('b')
    key(w.find('[data-msg-id="b"]'), { key: 'ArrowUp' })
    expect(document.activeElement.dataset.msgId).toBe('a')
  })

  it('Shift+F10 and the ContextMenu key open the message menu', async () => {
    setup()
    key(w.find('[data-msg-id="b"]'), { key: 'F10', shiftKey: true })
    await nextTick()
    expect(document.body.querySelector('[role="menu"]')).toBeTruthy()
    expect(document.body.querySelectorAll('[role="menuitem"]').length).toBeGreaterThan(3)
  })

  it('shows the hover bar for keyboard focus (focus-within)', () => {
    setup()
    const bar = w.find('[data-msg-id="a"] .absolute.right-4')
    expect(bar.classes()).toContain('group-focus-within:flex')
  })
})
