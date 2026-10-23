import { required } from '../store-test-support.fixture'
import { channelFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, ref, nextTick } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageScroll } from './useMessageScroll'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

let chat: ReturnType<typeof useChatStore>
let el: HTMLElement

// jsdom has no layout: give the container fixed metrics.
function metrics(node: HTMLElement, { scrollHeight = 2000, clientHeight = 500, scrollTop = 0 } = {}) {
  Object.defineProperty(node, 'scrollHeight', { value: scrollHeight, configurable: true })
  Object.defineProperty(node, 'clientHeight', { value: clientHeight, configurable: true })
  Object.defineProperty(node, 'clientWidth', { value: 300, configurable: true })
  node.scrollTop = scrollTop
}

function row(id: string, top: number) {
  const r = document.createElement('div')
  r.dataset.msgId = id
  r.getBoundingClientRect = () => ({ top, bottom: top + 40 }) as DOMRect
  r.scrollIntoView = vi.fn()
  el.appendChild(r)
  return r
}

// Mounted through a component so onMounted/onUnmounted run.
let wrapper: VueWrapper | undefined
function setup() {
  let api: ReturnType<typeof useMessageScroll> | undefined
  wrapper = mount(defineComponent({
    setup() { api = useMessageScroll(ref(el)); return () => null }
  }))
  return required(api)
}

const flush = async () => { await nextTick(); await nextTick() }

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me' })
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
  el = document.createElement('div')
  el.getBoundingClientRect = () => ({ top: 0, bottom: 500 }) as DOMRect
  document.body.appendChild(el)
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 1 })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('useMessageScroll', () => {
  it('lists the rows that carry a message id', () => {
    row('a', 0)
    el.appendChild(document.createElement('span'))
    expect(setup().messageRows().length).toBe(1)
  })

  it('scrollToBottomNow pins the view and clears the unread pill', () => {
    metrics(el)
    const s = setup()
    s.showUnreadPill.value = true
    s.scrollToBottomNow()
    expect(el.scrollTop).toBe(2000)
    expect(s.showUnreadPill.value).toBe(false)
    s.showUnreadPill.value = true
    s.resetUnreadPill()
    expect(s.showUnreadPill.value).toBe(false)
  })

  it('scrolls to the bottom when the latest page loads', async () => {
    metrics(el)
    const s = setup()
    chat.messages = [messageFixture({ id: 'm1' })]
    chat.latestLoadSeq++
    await flush()
    expect(el.scrollTop).toBe(2000)
    expect(s.showUnreadPill.value).toBe(false)
  })

  it('shows the unread pill for a live message while reading history', async () => {
    metrics(el)
    const s = setup()
    await flush()
    metrics(el, { scrollTop: 100 })
    chat.messages = [messageFixture({ id: 'm1', user_id: 'other' })]
    chat.liveAppendSeq++
    await flush()
    expect(s.showUnreadPill.value).toBe(true)
  })

  it('follows a live message when parked at the bottom, and always for own messages', async () => {
    metrics(el, { scrollTop: 1500 })
    const s = setup()
    await flush()
    metrics(el, { scrollTop: 1500 })
    chat.messages = [messageFixture({ id: 'm1', user_id: 'other' })]
    chat.liveAppendSeq++
    await flush()
    expect(s.showUnreadPill.value).toBe(false)

    metrics(el, { scrollTop: 100 })
    chat.messages = [...chat.messages, messageFixture({ id: 'm2', user_id: 'me' })]
    chat.liveAppendSeq++
    await flush()
    expect(el.scrollTop).toBe(2000)
  })

  it('keeps the first visible row in place when rows are added above', async () => {
    metrics(el, { scrollTop: 700 })
    let top = 10
    const r = row('a', 0)
    r.getBoundingClientRect = () => ({ top, bottom: top + 40 }) as DOMRect
    setup()
    await flush()
    metrics(el, { scrollTop: 700 })
    chat.messages = [messageFixture({ id: 'a' })]
    await nextTick()
    top = 110 // a page was prepended above
    await flush()
    expect(el.scrollTop).toBe(800)
  })

  it('jumps to a target message and highlights it for two seconds', async () => {
    vi.useFakeTimers()
    metrics(el)
    const r = row('t', 0)
    const s = setup()
    chat.jumpTarget = { id: 't', seq: 1 }
    await flush()
    expect(r.scrollIntoView).toHaveBeenCalledWith({ block: 'center' })
    vi.advanceTimersByTime(20) // fake timers also own requestAnimationFrame
    expect(s.highlightedId.value).toBe('t')
    vi.advanceTimersByTime(2000)
    expect(s.highlightedId.value).toBeNull()
  })

  it('ignores a jump to a message that is not rendered', async () => {
    metrics(el)
    const s = setup()
    chat.jumpTarget = { id: 'gone', seq: 1 }
    await flush()
    expect(s.highlightedId.value).toBeNull()
  })

  it('loads older and newer pages near either end', () => {
    metrics(el, { scrollTop: 100 })
    chat.messages = [messageFixture({ id: 'm1' })]
    chat.hasMoreBefore = true
    chat.hasMoreAfter = true
    const older = vi.spyOn(chat, 'loadOlder').mockResolvedValue(undefined as never)
    const newer = vi.spyOn(chat, 'loadNewer').mockResolvedValue(undefined as never)
    metrics(el, { scrollHeight: 800, scrollTop: 100 })
    setup().handleScroll()
    expect(older).toHaveBeenCalled()
    expect(newer).toHaveBeenCalled()
  })

  it('does not load while a window load is running', () => {
    metrics(el, { scrollTop: 0 })
    chat.messages = [messageFixture({ id: 'm1' })]
    chat.hasMoreBefore = true
    chat.isLoadingWindow = true
    const older = vi.spyOn(chat, 'loadOlder').mockResolvedValue(undefined as never)
    setup().handleScroll()
    expect(older).not.toHaveBeenCalled()
  })

  it('hides the unread pill once back at the bottom', () => {
    metrics(el, { scrollTop: 1500 })
    const s = setup()
    s.showUnreadPill.value = true
    s.handleScroll()
    expect(s.showUnreadPill.value).toBe(false)
  })

  it('re-pins the view when the container resizes while stuck to the bottom', async () => {
    let observed: (() => void) | undefined
    const disconnect = vi.fn()
    vi.stubGlobal('ResizeObserver', class {
      constructor(cb: () => void) { observed = cb }
      observe() {}
      disconnect() { disconnect() }
    })
    metrics(el)
    const s = setup()
    await flush()
    required(observed)()
    expect(el.scrollTop).toBe(2000)
    // Scrolled away: a resize no longer drags the view down.
    metrics(el, { scrollTop: 100 })
    s.handleScroll()
    Object.defineProperty(el, 'clientWidth', { value: 400, configurable: true })
    required(observed)()
    expect(el.scrollTop).toBe(100)
    wrapper?.unmount()
    expect(disconnect).toHaveBeenCalled()
  })
  it('keeps the bottom when an image arrives after the first scroll', async () => {
    metrics(el)
    const s = setup()
    await flush()
    s.scrollToBottomNow()
    metrics(el, { scrollHeight: 2400 }) // the image grew the list
    el.dispatchEvent(new Event('load'))
    expect(el.scrollTop).toBe(2400)
  })

  it('keeps a jump target centred while images above it load, until the person scrolls', async () => {
    metrics(el)
    const r = row('t', 0)
    setup()
    chat.jumpTarget = { id: 't', seq: 1 }
    await flush()
    vi.mocked(r.scrollIntoView).mockClear()
    el.dispatchEvent(new Event('load'))
    expect(r.scrollIntoView).toHaveBeenCalledWith({ block: 'center' })

    el.dispatchEvent(new Event('wheel'))
    vi.mocked(r.scrollIntoView).mockClear()
    el.dispatchEvent(new Event('load'))
    expect(r.scrollIntoView).not.toHaveBeenCalled()
  })
})
