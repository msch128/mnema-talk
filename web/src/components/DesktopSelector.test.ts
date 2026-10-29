import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
const calls = vi.hoisted(() => ({ open: vi.fn() }))
vi.mock('@tauri-apps/api/core', async importOriginal => ({ ...await importOriginal<typeof import('@tauri-apps/api/core')>(), invoke: calls.open }))
import DesktopSelector from './DesktopSelector.vue'
import { setLocale } from '../i18n'
beforeEach(() => { setLocale('de'); vi.clearAllMocks(); calls.open.mockResolvedValue(undefined) })
afterEach(() => vi.unstubAllGlobals())

it.each([false, true])('opens the normal web instance (failure: %s)', async failure => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL('http://tauri.localhost/'))
  vi.stubGlobal('__MNEMA_WEB_DESKTOP__', true)
  if (failure) calls.open.mockRejectedValue('This instance needs an update for the desktop client.')
  const wrapper = mount(DesktopSelector); await flushPromises()
  await wrapper.get('input').setValue('community.example.invalid')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(calls.open).toHaveBeenCalledExactlyOnceWith('desktop_open_instance', { address: 'community.example.invalid' })
  expect(wrapper.find('[role="alert"]').exists()).toBe(failure)
  if (failure) expect(wrapper.text()).toContain('This instance needs an update')
  wrapper.unmount()
})
it('shows the verified bundle in the selector of the trusted desktop client', async () => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL('mnema://localhost/'))
  vi.stubGlobal('__MNEMA_TRUSTED_APP__', { version: '0.8.0', source: 'cache', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'selector' })
  const wrapper = mount(DesktopSelector); await flushPromises()
  expect(wrapper.get('[data-testid="verified-app"]').text()).toBe('Geprüfte App v0.8.0 · geprüftes Update')
  wrapper.get('input').element.value = 'community.example.invalid'
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(calls.open).toHaveBeenCalledExactlyOnceWith('desktop_open_instance', { address: 'community.example.invalid' })
  wrapper.unmount()
})
it('shows the generic connection error when the host rejects without an explanation', async () => {
  calls.open.mockRejectedValue(new Error('synthetic-private-host-error'))
  const wrapper = mount(DesktopSelector); await flushPromises()
  wrapper.get('input').element.value = 'community.example.invalid'
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toBe('Dieser Server ist nicht erreichbar oder unterstützt diese DEV-Version nicht.')
  expect(wrapper.text()).not.toContain('synthetic-private-host-error')
  wrapper.unmount()
})
it('reads an autofilled address once and ignores a second submit while opening', async () => {
  let resolve!: (value: unknown) => void
  calls.open.mockImplementation(() => new Promise(r => { resolve = r }))
  const wrapper = mount(DesktopSelector); await flushPromises()
  wrapper.get('input').element.value = 'autofill.example.invalid'
  await wrapper.get('form').trigger('submit'); await wrapper.get('form').trigger('submit')
  expect(calls.open).toHaveBeenCalledExactlyOnceWith('desktop_open_instance', { address: 'autofill.example.invalid' })
  resolve(undefined); await flushPromises(); wrapper.unmount()
})
it.each([false, true])('ignores a desktop open that finishes after the selector is gone (failure: %s)', async failure => {
  let resolve!: (value: unknown) => void, reject!: (value: unknown) => void
  calls.open.mockImplementation(() => new Promise((r, j) => { resolve = r; reject = j }))
  const wrapper = mount(DesktopSelector); await flushPromises()
  wrapper.get('input').element.value = 'community.example.invalid'
  await wrapper.get('form').trigger('submit')
  wrapper.unmount()
  if (failure) reject('This instance needs an update for the desktop client.'); else resolve(undefined)
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
})
