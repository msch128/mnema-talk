import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarServerMenu from './SidebarServerMenu.vue'
import { useFirstRunStore } from '../stores/firstRun'
import { useAuthStore } from '../stores/auth'
import { channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { setLocale } from '../i18n'

enableAutoUnmount(afterEach)

const steps = [{ id: 'name', done: false, optional: false }]

function mountMenu() {
  return mount(SidebarServerMenu, { global: { directives: { tooltip: () => undefined } } })
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ role: 'admin' })
})

describe('the server menu and the Owner\'s checklist', () => {
  it('brings a closed checklist back', async () => {
    const store = useFirstRunStore()
    store.state = { steps, hidden: true, complete: false }
    store.setHidden = vi.fn().mockResolvedValue(undefined) as never
    const w = mountMenu()
    await w.get('button[aria-haspopup="menu"]').trigger('click')
    await w.get('[data-testid="menu-first-run"]').trigger('click')
    expect(store.setHidden).toHaveBeenCalledWith(false)
  })

  it('opens the first text channel when the list is reopened from a Talk', async () => {
    const store = useFirstRunStore()
    store.state = { steps, hidden: true, complete: false }
    store.setHidden = vi.fn().mockResolvedValue(undefined) as never
    const chat = useChatStore()
    chat.categories = [{ id: fixtureId(9), name: 'c', sort_order: 0, channels: [channelFixture({ id: fixtureId(1), type: 'voice' }), channelFixture({ id: fixtureId(2), type: 'text', name: 'general' })] }] as never
    chat.activeChannel = chat.allChannels[0]!
    chat.selectChannel = vi.fn().mockResolvedValue(undefined) as never
    const voice = useVoiceStore()
    voice.activeView = 'voice'
    const w = mountMenu()
    await w.get('button[aria-haspopup="menu"]').trigger('click')
    await w.get('[data-testid="menu-first-run"]').trigger('click')
    expect(chat.selectChannel).toHaveBeenCalledWith(expect.objectContaining({ id: fixtureId(2) }))
    expect(voice.activeView).toBe('chat')
  })

  it('offers nothing while the checklist is open or was never loaded', async () => {
    const store = useFirstRunStore()
    for (const state of [null, { steps, hidden: false, complete: false }]) {
      store.state = state
      const w = mountMenu()
      await w.get('button[aria-haspopup="menu"]').trigger('click')
      expect(w.find('[data-testid="menu-first-run"]').exists()).toBe(false)
      w.unmount()
    }
  })
})
