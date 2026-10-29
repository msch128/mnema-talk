import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarServerMenu from './SidebarServerMenu.vue'
import { useFirstRunStore } from '../stores/firstRun'
import { useAuthStore } from '../stores/auth'
import { userFixture } from '../test-fixtures.fixture'
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
