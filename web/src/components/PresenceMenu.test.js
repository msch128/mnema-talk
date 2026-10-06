import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import PresenceMenu from './PresenceMenu.vue'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'

vi.mock('../lib/api', () => ({
  api: vi.fn(() => Promise.resolve(null)),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

let auth, chat, trigger, wrapper

function mountMenu() {
  wrapper = mount(PresenceMenu, { props: { trigger }, attachTo: document.body })
  return wrapper
}

const option = (w, p) => w.find(`[data-presence="${p}"]`)
const key = (w, k) => w.find('[role="menu"]').trigger('keydown', { key: k })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  auth = useAuthStore()
  chat = useChatStore()
  auth.user = { id: 'me', username: 'me', presence: 'away', status_text: '' }
  chat.setMyPresence = vi.fn(async () => {})
  chat.openUserProfile = vi.fn()
  trigger = document.createElement('button')
  document.body.appendChild(trigger)
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('PresenceMenu', () => {
  it('marks the current presence and focuses it', async () => {
    const w = mountMenu()
    await flushPromises()
    expect(option(w, 'away').attributes('aria-checked')).toBe('true')
    expect(option(w, 'online').attributes('aria-checked')).toBe('false')
    expect(document.activeElement).toBe(option(w, 'away').element)
    expect(w.text()).toContain('Set a status message')
  })

  it('treats a user without presence as online', async () => {
    auth.user = { id: 'me', status_text: 'brb' }
    const w = mountMenu()
    await flushPromises()
    expect(option(w, 'online').attributes('aria-checked')).toBe('true')
    expect(w.text()).toContain('Edit status message')
  })

  it('saves a new presence, closes and returns focus', async () => {
    const w = mountMenu()
    await flushPromises()
    await option(w, 'dnd').trigger('click')
    await flushPromises()
    expect(chat.setMyPresence).toHaveBeenCalledWith('dnd')
    expect(w.emitted('close')).toHaveLength(1)
    expect(document.activeElement).toBe(trigger)
  })

  it('does not save the presence that is already set', async () => {
    const w = mountMenu()
    await option(w, 'away').trigger('click')
    await flushPromises()
    expect(chat.setMyPresence).not.toHaveBeenCalled()
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('reports a failed save', async () => {
    chat.setMyPresence = vi.fn().mockRejectedValueOnce(new Error('')).mockRejectedValueOnce(new Error('offline'))
    const w = mountMenu()
    await option(w, 'focus').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'error', text: "Couldn't save your status" })
    await option(w, 'online').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1).text).toBe('offline')
  })

  it('opens the profile to edit the status message', async () => {
    const w = mountMenu()
    await w.find('[role="menuitem"]').trigger('click')
    expect(chat.openUserProfile).toHaveBeenCalledWith(auth.user)
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('moves focus with the arrow keys and wraps around', async () => {
    const w = mountMenu()
    await flushPromises()
    const all = w.findAll('[role^="menuitem"]').map(x => x.element)
    expect(all).toHaveLength(5)
    await key(w, 'ArrowDown') // away -> dnd
    expect(document.activeElement).toBe(option(w, 'dnd').element)
    await key(w, 'ArrowDown')
    await key(w, 'ArrowDown') // -> status message
    expect(document.activeElement).toBe(all[4])
    await key(w, 'ArrowDown') // wraps to online
    expect(document.activeElement).toBe(all[0])
    await key(w, 'ArrowUp') // wraps back
    expect(document.activeElement).toBe(all[4])
  })

  it('closes on Escape with focus back on the trigger, and on Tab without', async () => {
    const w = mountMenu()
    await flushPromises()
    await key(w, 'Escape')
    expect(w.emitted('close')).toHaveLength(1)
    expect(document.activeElement).toBe(trigger)

    option(w, 'online').element.focus()
    await key(w, 'Tab')
    expect(w.emitted('close')).toHaveLength(2)
    expect(document.activeElement).toBe(option(w, 'online').element)
  })

  it('closes on a pointer down outside, but not inside or on the trigger', async () => {
    const w = mountMenu()
    await flushPromises()
    option(w, 'dnd').element.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    trigger.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(w.emitted('close')).toBeUndefined()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('stops listening for outside clicks once closed', async () => {
    const onClose = vi.fn()
    const w = mount(PresenceMenu, { props: { trigger, onClose }, attachTo: document.body })
    w.unmount()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(onClose).not.toHaveBeenCalled()
  })
})
