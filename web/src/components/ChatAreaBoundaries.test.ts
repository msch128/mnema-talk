import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import ChatArea from './ChatArea.vue'
import ContextMenu from './ContextMenu.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { fixtureId, channelFixture, messageFixture, readStateFixture, userFixture, memoryStorageFixture } from '../test-fixtures.fixture'
import { t } from '../i18n'

let wrapper: ReturnType<typeof mount<typeof ChatArea>> | undefined
const channelId = fixtureId(2)

function seed() {
  useAuthStore().user = userFixture()
  const chat = useChatStore()
  chat.activeChannel = channelFixture()
  chat.notificationPermission = 'granted'
  chat.messages = [messageFixture({ user_id: fixtureId(4), username: 'other' })]
  return chat
}

function render() {
  wrapper = mount(ChatArea, { attachTo: document.body })
  return wrapper
}

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.removeItem('mnema_notif_hint_dismissed')
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(null, { status: 204 })))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ChatArea boundary and teardown behavior', () => {
  it('keeps notification opt-in usable when reading browser storage is denied', async () => {
    const chat = seed()
    chat.notificationPermission = 'default'
    // Initialize stores before failing the component's own preference read.
    const read = vi.fn<(key: string) => string | null>(() => { throw new DOMException('Storage unavailable', 'SecurityError') })
    const write = vi.fn<(key: string, value: string) => void>(() => { throw new DOMException('Storage unavailable', 'SecurityError') })
    vi.stubGlobal('localStorage', { ...memoryStorageFixture(), getItem: read, setItem: write })
    const w = render()
    expect(read).toHaveBeenCalledWith('mnema_notif_hint_dismissed')
    expect(w.find('[role="region"]').exists()).toBe(true)
    await w.find(`[aria-label="${t('notifications.notNow')}"]`).trigger('click')
    expect(write).toHaveBeenCalledWith('mnema_notif_hint_dismissed', '1')
    expect(w.find('[role="region"]').exists()).toBe(false)
  })

  it('ignores a settings click whose channel was removed before the DOM updated', async () => {
    const chat = seed()
    const w = render()
    const button = w.find<HTMLButtonElement>('[aria-haspopup="menu"]').element
    chat.activeChannel = null
    button.click()
    await nextTick()
    expect(w.findComponent(ContextMenu).props('modelValue')).toBe(false)
    expect(w.find('[aria-haspopup="menu"]').exists()).toBe(false)
  })

  it('does not restore or paginate a timeline after its host unmounts during a queued update', async () => {
    const chat = seed()
    const older = vi.spyOn(chat, 'loadOlder').mockResolvedValue(true)
    const newer = vi.spyOn(chat, 'loadNewer').mockResolvedValue(true)
    const w = render()
    await flushPromises()
    const timeline = w.find<HTMLElement>('.overflow-y-auto').element
    Object.defineProperties(timeline, {
      scrollTop: { configurable: true, writable: true, value: 1000 },
      scrollHeight: { configurable: true, value: 3000 },
      clientHeight: { configurable: true, value: 600 }
    })
    const rectangle = vi.spyOn(timeline, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 100, 500, 600))
    vi.spyOn(w.find<HTMLElement>('[data-msg-id]').element, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 120, 100, 20))
    chat.messages = [messageFixture({ id: fixtureId(5), user_id: fixtureId(4) }), ...chat.messages]
    // Host teardown precedes the queued post-render anchor restoration.
    await nextTick()
    w.unmount()
    rectangle.mockClear()
    await flushPromises()
    expect(rectangle).not.toHaveBeenCalled()
    expect(older).not.toHaveBeenCalled()
    expect(newer).not.toHaveBeenCalled()
    expect(timeline.scrollTop).toBe(1000)
  })

  it('handles a pending resize notification after observer disconnection', async () => {
    let notify = () => {}
    const disconnected = vi.fn()
    vi.stubGlobal('ResizeObserver', class implements ResizeObserver {
      constructor(callback: ResizeObserverCallback) { notify = () => callback([], this) }
      observe = vi.fn()
      unobserve = vi.fn()
      disconnect = disconnected
    })
    seed()
    const w = render()
    await flushPromises()
    const timeline = w.find<HTMLElement>('.overflow-y-auto').element
    const rectangle = vi.spyOn(timeline, 'getBoundingClientRect')
    w.unmount()
    expect(disconnected).toHaveBeenCalledOnce()
    rectangle.mockClear()
    notify()
    expect(rectangle).not.toHaveBeenCalled()
  })

  it('renders and sends normally on browsers without ResizeObserver', async () => {
    vi.stubGlobal('ResizeObserver', undefined)
    const chat = seed()
    const send = vi.spyOn(chat, 'sendMessage').mockResolvedValue(null)
    const w = render()
    const composer = w.find('textarea')
    await composer.setValue('Supported without resize observation')
    await composer.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(send).toHaveBeenCalledWith('Supported without resize observation', null, null)
    expect(composer.element.value).toBe('')
  })

  it('handles global Escape from a non-element event target and marks the channel read', () => {
    const chat = seed()
    chat.readStates[channelId] = readStateFixture({ unread_count: 1 })
    const markRead = vi.spyOn(chat, 'markChannelRead').mockResolvedValue(undefined)
    render()
    const event = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true })
    window.dispatchEvent(event)
    expect(markRead).toHaveBeenCalledWith(channelId)
    expect(event.defaultPrevented).toBe(true)
  })

  it('safely handles a row event when the host closes the panel during capture', () => {
    seed()
    const w = render()
    const row = w.find<HTMLElement>('[data-msg-id]').element
    document.addEventListener('keydown', () => w.unmount(), { capture: true, once: true })
    const event = new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })
    row.dispatchEvent(event)
    expect(document.body.querySelector('[data-msg-id]')).toBeNull()
    expect(document.activeElement).not.toBe(row)
  })
})
