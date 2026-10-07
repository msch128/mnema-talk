import { requireValue } from '../test-fixtures.fixture'
import { messageFixture, userFixture, categoryFixture, channelFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import ChatArea from './ChatArea.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale, i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

let w!: ReturnType<typeof mount<typeof ChatArea>>
let calls: {url: string; method: string}[]

function msg(id: string, user: string, min: number, content = 'text ' + id) {
  return messageFixture({
    id, channel_id: 'ch1', user_id: user, username: user, display_name: user.toUpperCase(),
    content, created_at: new Date(Date.UTC(2026, 0, 1, 10, min)).toISOString(), attachments: [], reactions: []
  })
}

function setup({ lastReadAt = null, notifPermission = 'granted' }: { lastReadAt?: string | null; notifPermission?: NotificationPermission } = {}) {
  const auth = useAuthStore()
  auth.user = userFixture({ id: 'me', username: 'me', role: 'user' })
  const chat = useChatStore()
  chat.notificationPermission = notifPermission
  chat.categories = [categoryFixture({ id: 'c', channels: [channelFixture({ id: 'ch1', name: 'allgemein', type: 'text' })] })]
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'allgemein', type: 'text' })
  chat.activeChannelLastReadAt = lastReadAt
  chat.members = [userFixture({ id: 'u2', username: 'anna', display_name: 'Anna' })]
  chat.messages = [msg('a', 'u2', 0), msg('b', 'u2', 30), msg('c', 'me', 40), msg('d', 'u2', 50)]
  w = mount(ChatArea, {
    attachTo: document.body,
    global: { plugins: [i18nPlugin], directives: { tooltip } }
  })
  return chat
}

beforeEach(() => {
  localStorage.removeItem('mnema_notif_hint_dismissed')
  setLocale('de')
  setActivePinia(createPinia())
  calls = []
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    calls.push({ url: String(url), method: init?.method || 'GET' })
    return new Response(null, { status: 204 })
  }))
})

afterEach(() => {
  w?.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

const key = (el: { element: Element }, init: KeyboardEventInit) => el.element.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init }))

describe('ChatArea new-messages divider', () => {
  it('renders "Neu seit" before the first unread message of someone else', async () => {
    setup({ lastReadAt: new Date(Date.UTC(2026, 0, 1, 10, 35)).toISOString() })
    await nextTick()
    const sep = w.findAll<HTMLElement>('[role="separator"]')
    expect(sep).toHaveLength(1)
    expect(requireValue(sep[0]).text()).toMatch(/^Neu seit/)
    // Placed directly before message "d" (own message "c" is not "new").
    expect(requireValue(requireValue(sep[0]).element.nextElementSibling instanceof HTMLElement ? requireValue(sep[0]).element.nextElementSibling : null).getAttribute('data-msg-id')).toBe('d')
  })

  it('shows no divider without a last-read time', async () => {
    setup()
    await nextTick()
    expect(w.find<HTMLElement>('[role="separator"]').exists()).toBe(false)
  })

  it('Esc marks the channel read and removes the divider', async () => {
    setup({ lastReadAt: new Date(Date.UTC(2026, 0, 1, 10, 5)).toISOString() })
    await nextTick()
    expect(w.find<HTMLElement>('[role="separator"]').exists()).toBe(true)
    key(w.find<HTMLTextAreaElement>('textarea'), { key: 'Escape' })
    await flushPromises()
    expect(calls.some(c => c.method === 'POST' && c.url === '/api/channels/ch1/read')).toBe(true)
    expect(w.find<HTMLElement>('[role="separator"]').exists()).toBe(false)
  })

  it('Esc does nothing while the composer has text', async () => {
    setup({ lastReadAt: new Date(Date.UTC(2026, 0, 1, 10, 5)).toISOString() })
    await w.find<HTMLTextAreaElement>('textarea').setValue('halb fertig')
    calls.length = 0
    key(w.find<HTMLTextAreaElement>('textarea'), { key: 'Escape' })
    await flushPromises()
    expect(calls.filter(c => c.url.endsWith('/read'))).toHaveLength(0)
    expect(w.find<HTMLElement>('[role="separator"]').exists()).toBe(true)
  })
})

describe('ChatArea typing indicator', () => {
  it('names one, two and several typers', async () => {
    const chat = setup()
    chat.typingByChannel = { ch1: [{ user_id: 'u2', display_name: 'Anna', username: 'anna' }] }
    await nextTick()
    expect(w.find<HTMLElement>('[role="status"]').text()).toBe('Anna tippt …')
    chat.typingByChannel = { ch1: [{ user_id: 'u2', display_name: 'Anna', username: 'anna' }, { user_id: 'u3', username: 'ben', display_name: '' }] }
    await nextTick()
    expect(w.find<HTMLElement>('[role="status"]').text()).toBe('Anna und ben tippen …')
    chat.typingByChannel = { ch1: [{ user_id: 'u2', username: 'a', display_name: '' }, { user_id: 'u3', username: 'b', display_name: '' }, { user_id: 'u4', username: 'c', display_name: '' }] }
    await nextTick()
    expect(w.find<HTMLElement>('[role="status"]').text()).toBe('Mehrere Personen tippen …')
  })

  it('announces typing through the composer, but not for empty text', async () => {
    const chat = setup()
    const spy = vi.spyOn(chat, 'sendTyping')
    await w.find<HTMLTextAreaElement>('textarea').setValue('   ')
    expect(spy).not.toHaveBeenCalled()
    await w.find<HTMLTextAreaElement>('textarea').setValue('hallo')
    expect(spy).toHaveBeenCalledWith('ch1')
  })
})

describe('ChatArea notification opt-in', () => {
  it('asks only after a click on the enable button', async () => {
    const N = { permission: 'default', requestPermission: vi.fn(async () => 'granted') }
    vi.stubGlobal('Notification', N)
    setup({ notifPermission: 'default' })
    expect(N.requestPermission).not.toHaveBeenCalled()
    const btn = w.findAll<HTMLElement>('button').find(b => b.text() === 'Aktivieren')
    expect(btn).toBeTruthy()
    await requireValue(btn).trigger('click')
    await flushPromises()
    expect(N.requestPermission).toHaveBeenCalledTimes(1)
    expect(w.findAll<HTMLElement>('button').some(b => b.text() === 'Aktivieren')).toBe(false)
  })

  it('stays hidden when permission is already decided', () => {
    setup({ notifPermission: 'granted' })
    expect(w.findAll<HTMLElement>('button').some(b => b.text() === 'Aktivieren')).toBe(false)
  })
})

describe('ChatArea message keyboard handling', () => {
  it('r starts a reply but Ctrl/Cmd+R is left to the browser', async () => {
    setup()
    const row = w.find<HTMLElement>('[data-msg-id="a"]')
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
    w.find<HTMLElement>('[data-msg-id="a"]').element.focus()
    key(w.find<HTMLElement>('[data-msg-id="a"]'), { key: 'ArrowDown' })
    expect(requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).dataset.msgId).toBe('b')
    key(w.find<HTMLElement>('[data-msg-id="b"]'), { key: 'ArrowUp' })
    expect(requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).dataset.msgId).toBe('a')
  })

  it('Shift+F10 and the ContextMenu key open the message menu', async () => {
    setup()
    key(w.find<HTMLElement>('[data-msg-id="b"]'), { key: 'F10', shiftKey: true })
    await nextTick()
    expect(document.body.querySelector('[role="menu"]')).toBeTruthy()
    expect(document.body.querySelectorAll<HTMLElement>('[role="menuitem"]').length).toBeGreaterThan(3)
  })

  it('shows the hover bar for keyboard focus (focus-within)', () => {
    setup()
    const bar = w.find<HTMLElement>('[data-msg-id="a"] .absolute.right-4')
    expect(bar.classes()).toContain('group-focus-within:flex')
  })
})

describe('ChatArea message actions', () => {
  it('e on an own message opens the editor with focus in it', async () => {
    setup()
    const row = w.find<HTMLElement>('[data-msg-id="c"]')
    row.element.focus()
    key(row, { key: 'e' })
    await flushPromises()
    const editor = w.find<HTMLTextAreaElement>('[data-msg-id="c"] textarea')
    expect(editor.exists()).toBe(true)
    expect(document.activeElement).toBe(editor.element)
    // Escape in the editor cancels and hands focus back to the message.
    key(editor, { key: 'Escape' })
    await flushPromises()
    expect(w.find<HTMLTextAreaElement>('[data-msg-id="c"] textarea').exists()).toBe(false)
    expect(document.activeElement).toBe(w.find<HTMLElement>('[data-msg-id="c"]').element)
  })

  it('e on someone else\'s message does nothing', async () => {
    setup()
    const ev = new KeyboardEvent('keydown', { key: 'e', bubbles: true, cancelable: true })
    w.find<HTMLElement>('[data-msg-id="a"]').element.dispatchEvent(ev)
    await nextTick()
    expect(ev.defaultPrevented).toBe(false)
    expect(w.find<HTMLTextAreaElement>('[data-msg-id="a"] textarea').exists()).toBe(false)
  })

  it('opens the author profile from a keyboard-reachable button', async () => {
    const chat = setup()
    const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(async () => {})
    const name = w.find<HTMLElement>('[data-msg-id="a"] [data-testid="author-name"]')
    expect(name.element.tagName).toBe('BUTTON')
    await name.trigger('click')
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: 'a' }))
  })

  it('reaction picker closes on Escape and on a press outside', async () => {
    setup()
    const smile = () => w.find<HTMLElement>('[data-msg-id="b"] .reaction-picker-anchor button')
    await smile().trigger('click')
    expect(w.find<HTMLElement>('[data-testid="reaction-palette"]').exists()).toBe(true)
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="reaction-palette"]').exists()).toBe(false)

    await smile().trigger('click')
    w.find<HTMLElement>('[data-msg-id="a"]').element.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="reaction-palette"]').exists()).toBe(false)
  })

  it('"add reaction" in the message menu opens the picker on that message', async () => {
    setup()
    key(w.find<HTMLElement>('[data-msg-id="b"]'), { key: 'F10', shiftKey: true })
    await nextTick()
    const items = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')]
    requireValue(items[1]).dispatchEvent(new Event('pointerdown', { bubbles: true }))
    requireValue(items[1]).click()
    await flushPromises()
    expect(w.find<HTMLElement>('[data-msg-id="b"] [data-testid="reaction-palette"]').exists()).toBe(true)
  })

  it('renders video attachments as a player', async () => {
    const chat = setup()
    chat.messages = [{ ...msg('v', 'u2', 0), attachments: [{ id: 'att', url: '/m/v', mime_type: 'video/mp4', is_deleted: false, original_filename: 'clip.mp4', size_bytes: 10 }] }]
    await nextTick()
    expect(w.find<HTMLVideoElement>('[data-msg-id="v"] video').exists()).toBe(true)
  })

  it('clears the file input after a failed upload', async () => {
    const chat = setup()
    vi.spyOn(chat, 'uploadMedia').mockRejectedValue(new Error('zu groß'))
    const input = w.find<HTMLInputElement>('input[type="file"]').element
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    Object.defineProperty(input, 'files', { configurable: true, value: [file] })
    let value = 'C:\\fakepath\\a.png'
    Object.defineProperty(input, 'value', { configurable: true, get: () => value, set: v => { value = v } })
    input.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(chat.uploadMedia).toHaveBeenCalled()
    expect(value).toBe('')
  })
})

// Exercise the panel through its public UI and child component events; stores
// stay real, with network-facing methods controlled per scenario.
import MessageRow from './MessageRow.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import ImageLightbox from './ImageLightbox.vue'
import ContextMenu from './ContextMenu.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import EmojiButton from './EmojiButton.vue'
import { useToastStore } from '../stores/toast'
import { readStateFixture } from '../test-fixtures.fixture'
import { t } from '../i18n'

const composer = () => w.find<HTMLTextAreaElement>('textarea')
const timeline = () => w.find<HTMLElement>('.overflow-y-auto').element
const frame = async () => { await new Promise(resolve => requestAnimationFrame(resolve)); await flushPromises() }
function dimensions(el: HTMLElement, top = 1000, height = 3000, client = 600) {
  Object.defineProperties(el, {
    scrollTop: { configurable: true, writable: true, value: top },
    scrollHeight: { configurable: true, value: height },
    clientHeight: { configurable: true, value: client },
    clientWidth: { configurable: true, value: 500 },
  })
}
const rowComponent = (id: string) => requireValue(w.findAllComponents(MessageRow).find(row => row.props('msg').id === id))

describe('ChatArea composer, replies and lifecycle', () => {
  it('sends trimmed text, guards blank and concurrent submits, and preserves failed drafts', async () => {
    const chat = setup()
    let finish!: (message: null) => void
    const send = vi.spyOn(chat, 'sendMessage').mockImplementation(() => new Promise<null>(resolve => { finish = resolve }))
    await composer().setValue(' ')
    key(composer(), { key: 'Enter' })
    expect(send).not.toHaveBeenCalled()
    await composer().setValue(' hello ')
    key(composer(), { key: 'Enter', shiftKey: true })
    expect(send).not.toHaveBeenCalled()
    key(composer(), { key: 'Enter' })
    key(composer(), { key: 'Enter' })
    expect(send).toHaveBeenCalledTimes(1)
    expect(send).toHaveBeenCalledWith('hello', null, null)
    finish(null)
    await flushPromises()
    expect(composer().element.value).toBe('')
    send.mockRejectedValueOnce(new Error('')).mockRejectedValueOnce(new Error('server rejected'))
    await composer().setValue('save this')
    key(composer(), { key: 'Enter' })
    await flushPromises()
    expect(composer().element.value).toBe('save this')
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('chat.sendFailed'))
    key(composer(), { key: 'Enter' })
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toBe('server rejected')
  })

  it('replies via row actions, cancels through both UI and Escape, and reveals sent replies at the present', async () => {
    const chat = setup()
    const send = vi.spyOn(chat, 'sendMessage').mockResolvedValue(null)
    const latest = vi.spyOn(chat, 'jumpToLatest').mockResolvedValue(true)
    rowComponent('a').vm.$emit('reply')
    await nextTick()
    w.findComponent(ReplyComposerBar).vm.$emit('cancel')
    await nextTick()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
    rowComponent('a').vm.$emit('reply')
    await nextTick()
    key(composer(), { key: 'Escape' })
    await nextTick()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
    rowComponent('a').vm.$emit('reply')
    await nextTick()
    chat.hasMoreAfter = true
    await composer().setValue('reply')
    key(composer(), { key: 'Enter' })
    await flushPromises()
    expect(send).toHaveBeenCalledWith('reply', null, 'a')
    expect(latest).toHaveBeenCalledTimes(1)
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
  })

  it('uploads with the selected reply, blocks sends during upload and invokes the attachment picker', async () => {
    const chat = setup()
    const send = vi.spyOn(chat, 'sendMessage').mockResolvedValue(null)
    let complete!: (value: ReturnType<typeof messageFixture>) => void
    const upload = vi.spyOn(chat, 'uploadMedia').mockImplementation(() => new Promise(resolve => { complete = resolve }))
    rowComponent('b').vm.$emit('reply')
    await nextTick()
    const input = w.find<HTMLInputElement>('input[type=file]').element
    const pick = vi.spyOn(input, 'click').mockImplementation(() => {})
    const button = requireValue(w.findAll<HTMLElement>('button').find(b => b.find('svg.lucide-plus').exists()))
    await button.trigger('click')
    expect(pick).toHaveBeenCalledTimes(1)
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'image.png', { type: 'image/png' })] })
    input.dispatchEvent(new Event('change'))
    await composer().setValue('draft')
    key(composer(), { key: 'Enter' })
    expect(send).not.toHaveBeenCalled()
    expect(upload).toHaveBeenCalledWith(expect.any(File), '', null, 'b')
    complete(messageFixture())
    await flushPromises()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
  })

  it('clears edits and reply targets when switching channels and inserts pending mentions into existing drafts', async () => {
    const chat = setup()
    await composer().setValue(' hi ')
    chat.pendingMention = 'anna'
    await flushPromises()
    expect(composer().element.value).toBe('hi @anna ')
    expect(chat.pendingMention).toBe('')
    await composer().setValue('')
    chat.pendingMention = 'ben'
    await flushPromises()
    expect(composer().element.value).toBe('@ben ')
    rowComponent('c').vm.$emit('edit')
    rowComponent('a').vm.$emit('reply')
    await nextTick()
    chat.activeChannel = channelFixture({ id: 'ch2', name: 'next' })
    await flushPromises()
    expect(w.findComponent(ReplyComposerBar).exists()).toBe(false)
    expect(w.find('[data-msg-id="c"] textarea').exists()).toBe(false)
    expect(document.activeElement).toBe(composer().element)
  })

  it('routes row events to editing, reactions, profiles, threads, images and reply navigation', async () => {
    const chat = setup()
    const edit = vi.spyOn(chat, 'editMessage').mockResolvedValue(null)
    const reaction = vi.spyOn(chat, 'toggleReaction').mockResolvedValue([])
    const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(undefined)
    const thread = vi.spyOn(chat, 'openThread').mockResolvedValue(undefined)
    const jump = vi.spyOn(chat, 'jumpToMessage').mockResolvedValue(true)
    const row = rowComponent('c')
    row.vm.$emit('edit')
    await nextTick()
    row.vm.$emit('update:editText', 'changed')
    row.vm.$emit('save')
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('ch1', 'c', 'changed')
    row.vm.$emit('edit')
    await nextTick()
    row.vm.$emit('cancel-edit')
    row.vm.$emit('react', '👍')
    row.vm.$emit('open-profile')
    row.vm.$emit('open-thread')
    row.vm.$emit('toggle-picker', 'c')
    await nextTick()
    expect(row.props('pickerId')).toBe('c')
    row.vm.$emit('close-picker')
    row.vm.$emit('open-image', '/media/test-image')
    await flushPromises()
    expect(reaction).toHaveBeenCalledWith('c', '👍')
    expect(profile).toHaveBeenCalledWith(expect.objectContaining({ id: 'c' }))
    expect(thread).toHaveBeenCalledWith(expect.objectContaining({ id: 'c' }))
    expect(w.findComponent(ImageLightbox).props('src')).toBe('/media/test-image')
    w.findComponent(ImageLightbox).vm.$emit('close')
    row.vm.$emit('jump')
    await nextTick()
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('chat.messageNotFound'))
    chat.messages = chat.messages.map(m => m.id === 'c' ? { ...m, reply_to_id: 'a', reply_to: { id: 'a', deleted: false, content: 'old', username: 'u2' } } : m)
    await nextTick()
    rowComponent('c').vm.$emit('jump')
    expect(jump).toHaveBeenCalledWith('a')
    chat.messages = chat.messages.map(m => m.id === 'c' ? { ...m, reply_to: { id: 'a', deleted: true } } : m)
    await nextTick()
    rowComponent('c').vm.$emit('jump')
    expect(jump).toHaveBeenCalledTimes(1)
  })

  it('handles mention suggestion keyboard selection and cursor events without sending', async () => {
    const chat = setup()
    const send = vi.spyOn(chat, 'sendMessage').mockResolvedValue(null)
    await composer().setValue('@a')
    composer().element.selectionStart = 2
    await composer().trigger('click')
    expect(w.findComponent(MentionSuggestions).exists()).toBe(true)
    w.findComponent(MentionSuggestions).vm.$emit('hover', 0)
    key(composer(), { key: 'Enter' })
    await nextTick()
    expect(send).not.toHaveBeenCalled()
    expect(composer().element.value).toBe('@anna ')
    await composer().setValue('@a')
    composer().element.selectionStart = 2
    await composer().trigger('keyup', { key: 'ArrowLeft' })
    await composer().trigger('keyup', { key: 'ArrowRight' })
    w.findComponent(MentionSuggestions).vm.$emit('pick', requireValue(w.findComponent(MentionSuggestions).props('items')[0]))
    await nextTick()
    w.findComponent(EmojiButton).vm.$emit('pick', '🙂')
    await composer().trigger('blur')
    expect(composer().element.value).toContain('🙂')
  })
})

describe('ChatArea notification settings and presentation', () => {
  it.each(['denied', 'default'] as const)('handles permission result %s', async permission => {
    localStorage.removeItem('mnema_notif_hint_dismissed')
    const chat = setup({ notifPermission: 'default' })
    vi.spyOn(chat, 'requestNotificationPermission').mockResolvedValue(permission)
    await requireValue(w.findAll<HTMLElement>('button').find(b => b.text() === t('notifications.enable'))).trigger('click')
    await flushPromises()
    expect(w.find('[role=region]').exists()).toBe(permission === 'default')
    if (permission === 'denied') expect(useToastStore().toasts.at(-1)?.text).toBe(t('notifications.denied'))
  })

  it('dismisses hints even if storage access is denied', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('private') })
    const chat = setup({ notifPermission: 'default' })
    expect(chat.notificationPermission).toBe('default')
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('private') })
    await w.find(`[aria-label="${t('notifications.notNow')}"]`).trigger('click')
    expect(w.find('[role=region]').exists()).toBe(false)
    vi.restoreAllMocks()
  })

  it('persists channel notification choices through the menu and closes it on Escape', async () => {
    const chat = setup()
    const set = vi.spyOn(chat, 'setNotificationLevel').mockResolvedValue(undefined)
    for (const level of ['all', 'mentions', 'mute'] as const) {
      chat.readStates.ch1 = readStateFixture({ channel_id: 'ch1', notify_level: level })
      await nextTick()
      await w.find('[aria-haspopup=menu]').trigger('click')
      await nextTick()
      const menu = w.findComponent(ContextMenu)
      const item = requireValue(requireValue(menu.props('items')).find(item => item.label === t(`notifications.${level}`)))
      expect(item?.shortcut).toBe('✓')
      item.action?.()
      expect(set).toHaveBeenCalledWith('ch1', level)
      menu.vm.$emit('close')
      await nextTick()
    }
    await w.find('[aria-haspopup=menu]').trigger('click')
    key(composer(), { key: 'Escape' })
    await nextTick()
    expect(w.findComponent(ContextMenu).props('modelValue')).toBe(false)
  })

  it('renders text and voice empty/loading states, channel topic, member toggle and close', async () => {
    const chat = setup()
    chat.activeChannel = channelFixture({ id: 'ch1', name: 'general', topic: 'topic' })
    await nextTick()
    expect(w.text()).toContain('topic')
    const members = w.find('[aria-pressed]')
    await members.trigger('click')
    expect(chat.showMemberList).toBe(false)
    await members.trigger('click')
    expect(chat.showMemberList).toBe(true)
    chat.messages = []
    chat.isLoadingWindow = true
    await nextTick()
    expect(w.find('svg.animate-spin').exists()).toBe(true)
    chat.isLoadingWindow = false
    await nextTick()
    expect(w.text()).toContain(t('chat.emptyBody'))
    await w.setProps({ panel: true })
    expect(w.element.tagName).toBe('SECTION')
    expect(w.find('[data-testid=voice-chat-empty]').exists()).toBe(true)
    await w.find('[data-testid=voice-chat-close]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    chat.messages = [msg('a', 'u2', 0)]
    await nextTick()
    expect(w.text()).toContain(t('talk.chatBeginning', { channel: 'general' }))
    chat.activeChannel = null
    await nextTick()
    expect(w.find('[data-testid=chat-channel-name]').text()).toBe(t('chat.selectChannel'))
  })
})

describe('ChatArea history scrolling', () => {
  it('loads pages only near edges and prevents duplicate requests while loading', async () => {
    const chat = setup()
    const older = vi.spyOn(chat, 'loadOlder').mockResolvedValue(true)
    const newer = vi.spyOn(chat, 'loadNewer').mockResolvedValue(true)
    await flushPromises()
    const el = timeline()
    dimensions(el)
    chat.hasMoreBefore = true
    chat.hasMoreAfter = true
    el.scrollTop = 100
    el.dispatchEvent(new Event('scroll'))
    el.dispatchEvent(new Event('scroll'))
    await frame()
    expect(older).toHaveBeenCalledTimes(1)
    expect(newer).not.toHaveBeenCalled()
    el.scrollTop = 2300
    el.dispatchEvent(new Event('scroll'))
    await frame()
    expect(newer).toHaveBeenCalledTimes(1)
    chat.isLoadingBefore = true
    chat.isLoadingAfter = true
    dimensions(el, 0, 300, 300)
    el.dispatchEvent(new Event('scroll'))
    await frame()
    expect(older).toHaveBeenCalledTimes(1)
    expect(newer).toHaveBeenCalledTimes(1)
    chat.isLoadingWindow = true
    el.dispatchEvent(new Event('scroll'))
    await frame()
    chat.messages = []
    chat.isLoadingWindow = false
    await flushPromises()
    el.dispatchEvent(new Event('scroll'))
    await frame()
    expect(older).toHaveBeenCalledTimes(1)
  })

  it('shows unread pill for remote appends while scrolled up, clears at bottom and follows own sends/latest loads', async () => {
    const chat = setup()
    await flushPromises()
    const el = timeline()
    dimensions(el)
    chat.messages = [...chat.messages, msg('remote', 'u2', 51)]
    chat.liveAppendSeq++
    await flushPromises()
    expect(w.text()).toContain(t('chat.newMessages'))
    const pill = requireValue(w.findAll('button').find(b => b.text() === t('chat.newMessages')))
    await pill.trigger('click')
    expect(el.scrollTop).toBe(el.scrollHeight)
    dimensions(el)
    chat.messages = [...chat.messages, msg('remote2', 'u2', 52)]
    chat.liveAppendSeq++
    await flushPromises()
    dimensions(el, 2400)
    el.dispatchEvent(new Event('scroll'))
    await frame()
    expect(w.text()).not.toContain(t('chat.newMessages'))
    dimensions(el)
    chat.messages = [...chat.messages, msg('own', 'me', 53)]
    chat.liveAppendSeq++
    await flushPromises()
    expect(el.scrollTop).toBe(el.scrollHeight)
    dimensions(el)
    chat.latestLoadSeq++
    await flushPromises()
    expect(el.scrollTop).toBe(el.scrollHeight)
    chat.hasMoreAfter = true
    const latest = vi.spyOn(chat, 'jumpToLatest').mockResolvedValue(true)
    await nextTick()
    expect(w.text()).toContain(t('chat.viewingOlder'))
    chat.missedLiveCount = 2
    await nextTick()
    expect(w.text()).toContain(t('chat.newMessages'))
    await requireValue(w.findAll('button').find(b => b.text().includes(t('chat.jumpToEnd')))).trigger('click')
    expect(latest).toHaveBeenCalledTimes(1)
  })

  it('preserves visible anchors after prepending and restores surviving anchors after trims', async () => {
    const chat = setup()
    await flushPromises()
    const el = timeline()
    dimensions(el)
    let shifted = false
    vi.spyOn(el, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, 100, 500, 600))
    for (const id of ['a', 'b', 'c', 'd']) {
      const row = w.find<HTMLElement>(`[data-msg-id="${id}"]`).element
      vi.spyOn(row, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, (id === 'a' ? 50 : 120) + (shifted ? 60 : 0), 100, 20))
    }
    chat.messages = [msg('previous', 'u2', 0), ...chat.messages]
    await nextTick()
    shifted = true
    await flushPromises()
    expect(el.scrollTop).toBe(1060)
    chat.messages = chat.messages.filter(m => m.id !== 'b')
    await flushPromises()
    expect(el.scrollTop).toBe(1060)
    // Capture enough visible rows to stop after five candidates.
    chat.messages = Array.from({ length: 7 }, (_, i) => msg('many' + i, 'u2', i))
    await flushPromises()
    for (const row of w.findAll<HTMLElement>('[data-msg-id]')) vi.spyOn(row.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 150, 100, 20))
    chat.messages = [...chat.messages]
    await flushPromises()
  })

  it('scrolls to jump targets, flashes briefly and safely ignores unloaded targets', async () => {
    const chat = setup()
    await flushPromises()
    const scroll = vi.spyOn(w.find<HTMLElement>('[data-msg-id=a]').element, 'scrollIntoView').mockImplementation(() => {})
    chat.jumpTarget = { id: 'a', seq: 1 }
    await frame()
    expect(scroll).toHaveBeenCalledWith({ block: 'center' })
    expect(w.find('[data-msg-id=a]').classes()).toContain('msg-flash')
    await new Promise(resolve => setTimeout(resolve, 2050))
    await nextTick()
    expect(w.find('[data-msg-id=a]').classes()).not.toContain('msg-flash')
    chat.jumpTarget = { id: 'missing', seq: 2 }
    await flushPromises()
    expect(scroll).toHaveBeenCalledTimes(1)
    chat.jumpTarget = null
    await flushPromises()
  })

  it('keeps the bottom pinned across resizes, leaves history alone, and cancels frames on unmount', async () => {
    let notifyResize!: () => void
    const disconnect = vi.fn()
    vi.stubGlobal('ResizeObserver', class implements ResizeObserver {
      constructor(cb: ResizeObserverCallback) { notifyResize = () => cb([], this) }
      observe = vi.fn()
      unobserve = vi.fn()
      disconnect = disconnect
    })
    setup()
    await flushPromises()
    const el = timeline()
    dimensions(el, 100, 3000, 600)
    notifyResize()
    expect(el.scrollTop).toBe(3000)
    dimensions(el, 100, 3000, 400)
    el.dispatchEvent(new Event('scroll'))
    await frame()
    notifyResize()
    expect(el.scrollTop).toBe(3000)
    el.scrollTop = 100
    el.dispatchEvent(new Event('scroll'))
    await frame()
    dimensions(el, 100, 3000, 350)
    notifyResize()
    expect(el.scrollTop).toBe(100)
    notifyResize()
    const cancel = vi.spyOn(window, 'cancelAnimationFrame')
    el.dispatchEvent(new Event('scroll'))
    w.unmount()
    expect(disconnect).toHaveBeenCalledTimes(1)
    expect(cancel).toHaveBeenCalled()
  })
})

describe('ChatArea Escape guards', () => {
  it('marks read for unread counts or mentions and skips already read/no channel', async () => {
    const chat = setup()
    const read = vi.spyOn(chat, 'markChannelRead').mockResolvedValue(undefined)
    const escape = () => document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    escape()
    expect(read).not.toHaveBeenCalled()
    chat.readStates.ch1 = readStateFixture({ channel_id: 'ch1', unread_count: 2 })
    escape()
    expect(read).toHaveBeenCalledTimes(1)
    chat.readStates.ch1 = readStateFixture({ channel_id: 'ch1', mention_count: 2 })
    escape()
    expect(read).toHaveBeenCalledTimes(2)
    chat.activeChannel = null
    escape()
    expect(read).toHaveBeenCalledTimes(2)
  })

  it('ignores Escape used by other controls, dialogs, lightboxes, editing and reply targets', async () => {
    const chat = setup()
    chat.readStates.ch1 = readStateFixture({ channel_id: 'ch1', unread_count: 2 })
    const read = vi.spyOn(chat, 'markChannelRead').mockResolvedValue(undefined)
    const other = document.createElement('input')
    document.body.append(other)
    other.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    document.body.append(dialog)
    key(composer(), { key: 'Escape' })
    dialog.remove()
    const used = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    used.preventDefault()
    composer().element.dispatchEvent(used)
    rowComponent('a').vm.$emit('open-image', '/image')
    await nextTick()
    key(composer(), { key: 'Escape' })
    await nextTick()
    rowComponent('c').vm.$emit('edit')
    await nextTick()
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    key(w.find('[data-msg-id=c]'), { key: 'Escape' })
    await nextTick()
    rowComponent('a').vm.$emit('reply')
    await nextTick()
    document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    key(w.find('[data-msg-id=a]'), { key: 'Escape' })
    await nextTick()
    expect(read).not.toHaveBeenCalled()
    key(w.find('[data-msg-id=a]'), { key: 'Escape' })
    expect(read).toHaveBeenCalledTimes(1)
    key(w.find('[data-msg-id=a]'), { key: 'F10', shiftKey: true })
    await nextTick()
    key(w.find('[data-msg-id=a]'), { key: 'Escape' })
    await nextTick()
    expect(w.findComponent(ContextMenu).props('modelValue')).toBe(false)
  })
})

describe('ChatArea message menu trigger paths', () => {
  it('opens menus from right-click and quick action without losing keyboard controls', async () => {
    setup()
    await w.find('[data-msg-id=a]').trigger('contextmenu', { clientX: 60, clientY: 80 })
    expect(w.findComponent(ContextMenu).props('modelValue')).toBe(true)
    w.findComponent(ContextMenu).vm.$emit('close')
    await nextTick()
    const more = requireValue(w.find('[data-msg-id=a]').findAll('button').find(button => button.find('svg.lucide-ellipsis').exists()))
    await more.trigger('click')
    expect(w.findComponent(ContextMenu).props('modelValue')).toBe(true)
  })

  it('renders the same-day unread divider and preserves grouping boundary', async () => {
    const now = new Date()
    now.setHours(10, 0, 0, 0)
    const chat = setup({ lastReadAt: now.toISOString() })
    chat.messages = [
      { ...msg('old', 'u2', 0), created_at: new Date(now.getTime() - 60000).toISOString() },
      { ...msg('new', 'u2', 1), created_at: new Date(now.getTime() + 60000).toISOString() },
    ]
    await nextTick()
    expect(w.find('[role=separator]').text()).toContain(t('chat.newSinceDivider', { time: now.toLocaleTimeString('de', { hour: '2-digit', minute: '2-digit' }) }))
    expect(rowComponent('new').props('grouped')).toBe(false)
  })
})
