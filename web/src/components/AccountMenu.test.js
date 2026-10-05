import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import UserBar from './UserBar.vue'
import { useAuthStore } from '../stores/auth'
import { setLocale, locale } from '../i18n'

vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn() })
}))

let w
beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn((url, init) => Promise.resolve({
    status: 200, ok: true,
    json: () => Promise.resolve({ id: 'u1', display_name: 'Herzog', role: 'admin', locale: JSON.parse(init.body).locale })
  })))
})
afterEach(() => {
  w?.unmount()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('account menu language switcher', () => {
  it('switches the language, stores it and confirms with a toast', async () => {
    const auth = useAuthStore()
    auth.user = { id: 'u1', display_name: 'Herzog', role: 'admin', locale: 'de' }
    w = mount(UserBar, { attachTo: document.body })
    await w.find('[data-testid="account-menu-button"]').trigger('click')
    const trigger = w.find('[data-lang-trigger]')
    expect(trigger.text()).toContain('Sprache')
    expect(trigger.text()).toContain('Deutsch')
    await trigger.trigger('click')
    const radios = w.findAll('[role="menuitemradio"]')
    expect(radios.map(r => r.text())).toEqual(['Deutsch', 'English'])
    expect(radios[0].attributes('aria-checked')).toBe('true')
    await radios[1].trigger('click')
    await vi.waitFor(() => expect(locale.value).toBe('en'))
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ locale: 'en' })
    expect(fetch.mock.calls[0][0]).toBe('/api/users/me/locale')
    expect(document.documentElement.lang).toBe('en')
    await nextTick()
    // menu closed after choosing
    expect(w.find('[role="menu"]').exists()).toBe(false)
  })

  it('closes on Escape and returns focus to the ⋯ button', async () => {
    const auth = useAuthStore()
    auth.user = { id: 'u1', display_name: 'Herzog', role: 'user', locale: 'de' }
    w = mount(UserBar, { attachTo: document.body })
    const btn = w.find('[data-testid="account-menu-button"]')
    await btn.trigger('click')
    await nextTick()
    await nextTick()
    const menu = w.find('[role="menu"]')
    expect(menu.exists()).toBe(true)
    // admin-only entry is absent for members
    expect(menu.text()).not.toContain('Admin-Konsole')
    menu.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(w.find('[role="menu"]').exists()).toBe(false)
    expect(document.activeElement).toBe(btn.element)
  })
})
