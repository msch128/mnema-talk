import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '../i18n'

const pwa = vi.hoisted(() => ({ state: 'unavailable', prompt: vi.fn(async () => true) }))
vi.mock('../lib/pwa', () => ({ installState: () => pwa.state, promptInstall: pwa.prompt }))

import AppSettingsModal from './AppSettingsModal.vue'
import { useToastStore } from '../stores/toast'

let w: VueWrapper
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

function open(state: string) {
  pwa.state = state
  w = mount(AppSettingsModal, { attachTo: document.body })
}

describe('installing the app from the dialog', () => {
  it('offers the install button when the browser can install the app', async () => {
    open('available')
    expect(w.find('[data-testid="app-install-state"]').text()).toContain('Install Mnema Talk')
    await w.find('[data-testid="app-install-button"]').trigger('click')
    expect(pwa.prompt).toHaveBeenCalled()
    await vi.waitFor(() => expect(useToastStore().toasts.some(x => x.text === 'Mnema Talk is installed.')).toBe(true))
  })

  it('stays quiet when the person dismisses the browser prompt', async () => {
    pwa.prompt.mockResolvedValueOnce(false)
    open('available')
    await w.find('[data-testid="app-install-button"]').trigger('click')
    await vi.waitFor(() => expect(pwa.prompt).toHaveBeenCalled())
    expect(useToastStore().toasts).toEqual([])
  })

  it('says how to add the app on iPhone and iPad, without a button', () => {
    open('ios')
    expect(w.find('[data-testid="app-install-state"]').text()).toContain('Add to Home Screen')
    expect(w.find('[data-testid="app-install-button"]').exists()).toBe(false)
  })

  it('says so when the app is already installed', () => {
    open('installed')
    expect(w.find('[data-testid="app-install-state"]').text()).toContain('is installed')
    expect(w.find('[data-testid="app-install-button"]').exists()).toBe(false)
  })

  it('points to the browser menu when it offers no install', () => {
    open('unavailable')
    expect(w.find('[data-testid="app-install-state"]').text()).toContain('does not offer to install')
  })

  it('closes with the button and with the header cross', async () => {
    open('unavailable')
    await w.find('footer button').trigger('click')
    await w.find('[data-dialog-close]').trigger('click')
    expect(w.emitted('close')).toHaveLength(2)
  })
})
