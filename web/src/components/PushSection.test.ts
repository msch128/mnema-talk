import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const lib = vi.hoisted(() => ({
  availability: 'ready' as 'ready' | 'needs-install' | 'unsupported',
  config: { enabled: true, public_key: 'AQID', preview_allowed: true, preview: true, max_devices: 10 },
  devices: [] as Array<{ id: string, label: string, fingerprint: string, created_at: string, last_success_at: string | null }>,
  mine: null as null | { id: string },
  enable: vi.fn(async () => 'enabled' as 'enabled' | 'denied' | 'unavailable'),
  remove: vi.fn(async () => {}),
  test: vi.fn(async () => ({ delivered: 1, failed: 0 })),
  preview: vi.fn(async () => {}),
  loadFails: false,
}))

vi.mock('../lib/webPush', () => ({
  pushAvailability: () => lib.availability,
  loadConfig: vi.fn(async () => { if (lib.loadFails) throw new Error('offline'); return lib.config }),
  loadDevices: vi.fn(async () => lib.devices),
  thisDevice: vi.fn(async () => lib.mine),
  refreshPushState: vi.fn(async () => true),
  enablePush: lib.enable,
  removeDevice: lib.remove,
  sendTestPush: lib.test,
  setPreview: lib.preview,
}))

import PushSection from './PushSection.vue'

const device = (id: string, label: string) => ({ id, label, fingerprint: id, created_at: '2026-10-20T10:00:00Z', last_success_at: null })

let w: VueWrapper
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  Object.assign(lib, { availability: 'ready', devices: [], mine: null, loadFails: false })
  lib.config = { enabled: true, public_key: 'AQID', preview_allowed: true, preview: true, max_devices: 10 }
  vi.stubGlobal('Notification', { permission: 'default' })
  for (const fn of [lib.enable, lib.remove, lib.test, lib.preview]) fn.mockClear()
})
afterEach(() => {
  w?.unmount()
  vi.unstubAllGlobals()
})

async function open() {
  w = mount(PushSection)
  await flushPromises()
}

const toasts = () => useToastStore().toasts.map(x => x.text)

describe('the push section', () => {
  it('shows a loading line, then why push is not available', async () => {
    w = mount(PushSection)
    expect(w.find('[data-testid="push-loading"]').exists()).toBe(true)
    await flushPromises()
    expect(w.find('[data-testid="push-loading"]').exists()).toBe(false)
  })

  it('says so when the operator turned push off', async () => {
    lib.config = { ...lib.config, enabled: false, public_key: '' }
    await open()
    expect(w.find('[data-testid="push-off"]').text()).toContain('operator turned push notifications off')
    expect(w.find('[data-testid="push-enable"]').exists()).toBe(false)
  })

  it('explains the home screen step on iPhone and iPad, and the lack of support elsewhere', async () => {
    lib.availability = 'needs-install'
    await open()
    expect(w.find('[data-testid="push-needs-install"]').text()).toContain('Add to Home Screen')
    w.unmount()
    lib.availability = 'unsupported'
    await open()
    expect(w.find('[data-testid="push-unsupported"]').text()).toContain('can’t receive push')
  })

  it('reports a failed load without hiding the rest of the dialog', async () => {
    lib.loadFails = true
    await open()
    expect(w.find('[data-testid="push-failed"]').exists()).toBe(true)
  })

  it('turns push on from a click and confirms', async () => {
    await open()
    expect(w.find('[data-testid="push-state"]').text()).toContain('Get notified on this device')
    await w.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    expect(lib.enable).toHaveBeenCalledWith(lib.config)
    expect(toasts()).toContain('Push notifications are on for this device.')
  })

  it('tells when the person refused the permission, and when push is unavailable', async () => {
    await open()
    lib.enable.mockResolvedValueOnce('denied')
    await w.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    lib.enable.mockResolvedValueOnce('unavailable')
    await w.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    expect(toasts()).toEqual(['Notifications are blocked in the browser.', 'Push notifications are not available here.'])
  })

  it('reports a failure of turning it on', async () => {
    await open()
    lib.enable.mockRejectedValueOnce(new Error('no network'))
    await w.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    expect(toasts()).toEqual(['no network'])
  })

  it('shows the block right after the person refuses the browser prompt', async () => {
    const notification = { permission: 'default' }
    vi.stubGlobal('Notification', notification)
    await open()
    expect(w.find('[data-testid="push-enable"]').attributes('disabled')).toBeUndefined()
    lib.enable.mockImplementationOnce(async () => { notification.permission = 'denied'; return 'denied' })
    await w.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="push-state"]').text()).toContain('blocked for this site')
    expect(w.find('[data-testid="push-enable"]').attributes('disabled')).toBeDefined()
  })

  it('does not offer to turn on what the browser blocks', async () => {
    vi.stubGlobal('Notification', { permission: 'denied' })
    await open()
    expect(w.find('[data-testid="push-state"]').text()).toContain('blocked for this site')
    expect(w.find('[data-testid="push-enable"]').attributes('disabled')).toBeDefined()
  })

  it('on a device with push on: test, turn off, and the devices list', async () => {
    lib.devices = [device('d1', 'Firefox on Linux'), device('d2', 'Safari on iOS')]
    lib.mine = { id: 'd1' }
    await open()
    expect(w.find('[data-testid="push-state"]').text()).toContain('even when Mnema Talk is closed')
    expect(w.findAll('[data-testid="push-device"]').map(d => d.text())).toEqual([
      expect.stringContaining('Firefox on Linux'), expect.stringContaining('Safari on iOS'),
    ])
    expect(w.findAll('[data-testid="push-device"]')[0]!.text()).toContain('This device')
    expect(w.findAll('[data-testid="push-device"]')[1]!.text()).not.toContain('This device')

    await w.find('[data-testid="push-test"]').trigger('click')
    await flushPromises()
    expect(toasts()).toContain('Test notification sent.')
    lib.test.mockResolvedValueOnce({ delivered: 0, failed: 1 })
    await w.find('[data-testid="push-test"]').trigger('click')
    await flushPromises()
    expect(toasts()).toContain('Couldn’t deliver the test notification. Try again.')

    await w.find('[data-testid="push-disable"]').trigger('click')
    await flushPromises()
    expect(lib.remove).toHaveBeenCalledWith(lib.mine)
  })

  it('refreshes in place after an action, keeping the buttons mounted', async () => {
    lib.devices = [device('d1', 'Firefox on Linux')]
    lib.mine = { id: 'd1' }
    await open()
    const button = w.find('[data-testid="push-test"]')
    const seen: boolean[] = []
    const watch = setInterval(() => seen.push(w.find('[data-testid="push-loading"]').exists()), 0)
    await button.trigger('click')
    await flushPromises()
    clearInterval(watch)
    expect(seen).not.toContain(true)
    expect(w.find('[data-testid="push-test"]').element).toBe(button.element)
  })

  it('removes another device from the list', async () => {
    lib.devices = [device('d1', 'Firefox on Linux'), device('d2', '')]
    lib.mine = { id: 'd1' }
    await open()
    const buttons = w.findAll('[data-testid="push-remove"]')
    expect(buttons[1]!.attributes('aria-label')).toBe('Remove Unnamed device')
    await buttons[1]!.trigger('click')
    await flushPromises()
    expect(lib.remove).toHaveBeenCalledWith(lib.devices[1])
  })

  it('reports a device that could not be removed', async () => {
    lib.devices = [device('d1', 'Firefox')]
    lib.mine = { id: 'd1' }
    await open()
    lib.remove.mockRejectedValueOnce(new Error('server error'))
    await w.find('[data-testid="push-remove"]').trigger('click')
    await flushPromises()
    expect(toasts()).toEqual(['server error'])
  })

  it('switches the message text on and off', async () => {
    lib.mine = { id: 'd1' }
    lib.devices = [device('d1', 'Firefox')]
    await open()
    const toggle = w.find('[data-testid="push-preview"]')
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click')
    await flushPromises()
    expect(lib.preview).toHaveBeenCalledWith(false)
    expect(toggle.attributes('aria-checked')).toBe('false')
    lib.preview.mockRejectedValueOnce(new Error('nope'))
    await toggle.trigger('click')
    await flushPromises()
    expect(toasts()).toContain('nope')
    expect(toggle.attributes('aria-checked')).toBe('false')
  })

  it('shows the switch as off and locked when the operator forbids message text', async () => {
    lib.config = { ...lib.config, preview_allowed: false }
    lib.mine = { id: 'd1' }
    lib.devices = [device('d1', 'Firefox')]
    await open()
    const toggle = w.find('[data-testid="push-preview"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(toggle.attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="push-preview-hint"]').text()).toContain('operator turned off message text')
  })
})
