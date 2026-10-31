import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

const app = vi.hoisted(() => ({ current: null as null | { role: string } }))
const report = vi.hoisted(() => vi.fn())
vi.mock('../lib/platform', async importOriginal => {
  const real = await importOriginal<typeof import('../lib/platform')>()
  return { ...real, platform: { ...real.platform, trustedApp: () => app.current, reportUnread: report, supportsUnreadReport: () => true } }
})

import { MARK_ALL_READ_EVENT, useDesktopMarkRead } from './useDesktopMarkRead'
import { useAppBadge } from './useAppBadge'
import { useChatStore } from '../stores/chat'

let mounted: App | null = null
function mount(pinia = createPinia(), use: () => void = useDesktopMarkRead) {
  mounted = createApp(defineComponent({ setup() { use(); return () => h('div') } }))
  mounted.use(pinia)
  mounted.mount(document.createElement('div'))
  return mounted
}
const fire = () => window.dispatchEvent(new CustomEvent(MARK_ALL_READ_EVENT))

beforeEach(() => { setActivePinia(createPinia()); app.current = { role: 'instance' } })
afterEach(() => { mounted?.unmount(); mounted = null })

describe('useDesktopMarkRead', () => {
  it('marks everything read when the desktop client asks, in the page of a server', () => {
    const instance = mount()
    const chat = useChatStore(instance.config.globalProperties.$pinia)
    const spy = vi.spyOn(chat, 'markAllRead').mockResolvedValue(true)
    fire()
    expect(spy).toHaveBeenCalledTimes(1)
    expect(spy).toHaveBeenCalledWith()
  })

  it('ignores the event in a browser and in the other pages of the client', () => {
    for (const current of [null, { role: 'selector' }]) {
      app.current = current
      const instance = mount()
      const spy = vi.spyOn(useChatStore(instance.config.globalProperties.$pinia), 'markAllRead').mockResolvedValue(true)
      fire()
      expect(spy).not.toHaveBeenCalled()
      mounted?.unmount()
      mounted = null
    }
  })

  it('stops listening when the page goes away', () => {
    const instance = mount()
    const spy = vi.spyOn(useChatStore(instance.config.globalProperties.$pinia), 'markAllRead').mockResolvedValue(true)
    instance.unmount()
    mounted = null
    fire()
    expect(spy).not.toHaveBeenCalled()
  })

  it('leaves the new counts to the report the page already makes when the read states change', async () => {
    report.mockClear()
    const pinia = createPinia()
    setActivePinia(pinia)
    const chat = useChatStore()
    chat.readStates = { a: { channel_id: 'a', notify_level: 'all', unread_count: 3, mention_count: 1, last_read_at: null } }
    mount(pinia, useAppBadge)
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ unread: 1, mentions: 1 })
    vi.spyOn(chat, 'markAllRead').mockImplementation(async () => { chat.readStates = { a: { ...chat.readStates.a!, unread_count: 0, mention_count: 0 } }; return true })
    fire()
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ unread: 0, mentions: 0 })
  })
})
