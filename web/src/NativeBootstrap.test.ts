import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
const calls = vi.hoisted(() => ({ open: vi.fn(), initialize: vi.fn(), connect: vi.fn(), disconnect: vi.fn(), closeSocket: vi.fn(), leaveVoice: vi.fn(), resetSession: vi.fn() }))
vi.mock('@tauri-apps/api/core', async importOriginal => ({ ...await importOriginal<typeof import('@tauri-apps/api/core')>(), invoke: calls.open }))
vi.mock('./lib/nativeTransport', () => ({ initializeNativeContext: calls.initialize, connectNative: calls.connect, disconnectNative: calls.disconnect }))
vi.mock('./App.vue', () => ({ default: { template: '<div data-testid="native-community-app">Community</div>' } }))
vi.mock('./stores/chat', () => ({ useChatStore: () => ({ resetCommunityState: calls.closeSocket }) }))
vi.mock('./stores/voice', () => ({ useVoiceStore: () => ({ disconnect: calls.leaveVoice }) }))
vi.mock('./stores/auth', () => ({ useAuthStore: () => ({ user: null, resetLocalSession: calls.resetSession }) }))
import NativeBootstrap from './NativeBootstrap.vue'
import { setLocale } from './i18n'
beforeEach(() => { setLocale('de'); vi.clearAllMocks(); calls.initialize.mockResolvedValue(undefined); calls.disconnect.mockResolvedValue(undefined) })
it('keeps the community/login surface unmounted until discovery completes', async () => {
  let resolve!: (value: unknown) => void
  calls.connect.mockImplementation(() => new Promise(r => { resolve = r }))
  const wrapper = mount(NativeBootstrap); await flushPromises()
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false)
  await wrapper.get('input').setValue('community.example.invalid'); await wrapper.get('form').trigger('submit')
  expect(calls.connect).toHaveBeenCalledWith('community.example.invalid')
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false)
  resolve({ status: 200, body: { origin: 'https://community.example.invalid', compatibility: 'supported', transport_preview: true, content_authorization: 'unavailable' } }); await flushPromises()
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(true)
  expect(wrapper.text()).toContain('Entwicklungsversion'); wrapper.unmount()
})
it.each(['unsupported', 'insecure'])('rejects %s discovery without exposing the login surface', async mode => {
  calls.connect.mockResolvedValue({ status: 200, body: { origin: mode === 'insecure' ? 'http://community.example.invalid' : 'https://community.example.invalid', compatibility: mode === 'unsupported' ? 'unsupported' : 'supported', transport_preview: true, content_authorization: 'unavailable' } })
  const wrapper = mount(NativeBootstrap); await flushPromises(); await wrapper.get('input').setValue('community.example.invalid'); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false); expect(wrapper.find('[role="alert"]').exists()).toBe(true); wrapper.unmount()
})
it('shows a fixed local error and makes no discovery request when native context fails', async () => {
  calls.initialize.mockRejectedValue(new Error('synthetic-private-native-error'))
  const wrapper = mount(NativeBootstrap); await flushPromises()
  expect(wrapper.text()).not.toContain('synthetic-private-native-error'); expect(wrapper.get('input').attributes('disabled')).toBeDefined(); expect(calls.connect).not.toHaveBeenCalled(); wrapper.unmount()
})
const discovered = { status: 200, body: { origin: 'https://community.example.invalid', compatibility: 'supported', transport_preview: true, content_authorization: 'unavailable' } }
it.each([false, true])('retires chat, voice and authentication before changing instance (disconnect failure: %s)', async failure => {
  calls.connect.mockResolvedValue(discovered)
  if (failure) calls.disconnect.mockRejectedValue(new Error('synthetic private failure'))
  const wrapper = mount(NativeBootstrap); await flushPromises()
  await wrapper.get('input').setValue('community.example.invalid'); await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('aside button').trigger('click'); await flushPromises()
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false)
  for (const call of [calls.closeSocket, calls.leaveVoice, calls.resetSession]) {
    expect(call).toHaveBeenCalledOnce()
    expect(call.mock.invocationCallOrder[0]).toBeLessThan(calls.disconnect.mock.invocationCallOrder[0]!)
  }
  expect(wrapper.find('[role="alert"]').exists()).toBe(failure)
  expect(wrapper.text()).not.toContain('synthetic private failure')
  expect(wrapper.get('input').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
it('reads an autofilled visible address synchronously and suppresses duplicate discovery', async () => {
  let resolve!: (value: unknown) => void
  calls.connect.mockImplementation(() => new Promise(r => { resolve = r }))
  const wrapper = mount(NativeBootstrap); await flushPromises()
  wrapper.get('input').element.value = 'autofill.example.invalid'
  await wrapper.get('form').trigger('submit'); await wrapper.get('form').trigger('submit')
  expect(calls.connect).toHaveBeenCalledExactlyOnceWith('autofill.example.invalid')
  resolve(discovered); await flushPromises(); wrapper.unmount()
})
it('keeps discovery disabled while native context is pending', async () => {
  let resolve!: () => void
  calls.initialize.mockImplementation(() => new Promise<void>(r => { resolve = r }))
  const wrapper = mount(NativeBootstrap)
  await wrapper.get('form').trigger('submit')
  expect(calls.connect).not.toHaveBeenCalled()
  resolve(); await flushPromises(); wrapper.unmount()
})
it.each([
  { status: 503, body: discovered.body }, { status: 200, body: null }, { status: 200, body: {} },
  { status: 200, body: { ...discovered.body, origin: 42 } },
  { status: 200, body: { ...discovered.body, transport_preview: false } },
  { status: 200, body: { ...discovered.body, content_authorization: 'plaintext' } },
  { status: 200, body: { ...discovered.body, origin: 'https://community.example.invalid/path' } },
  { status: 200, body: { ...discovered.body, origin: 'https://user:synthetic@community.example.invalid' } },
  { status: 200, body: { ...discovered.body, origin: 'invalid url' } }
])('never mounts the shared login on malformed or incompatible discovery', async reply => {
  calls.connect.mockResolvedValue(reply)
  const wrapper = mount(NativeBootstrap); await flushPromises()
  await wrapper.get('input').setValue('community.example.invalid'); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false)
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  wrapper.unmount()
})
it.each([false, true])('retires delayed discovery on unmount (native failure: %s)', async failure => {
  let resolve!: (value: unknown) => void, reject!: (value: unknown) => void
  calls.connect.mockImplementation(() => new Promise((r, j) => { resolve = r; reject = j }))
  const wrapper = mount(NativeBootstrap); await flushPromises()
  await wrapper.get('input').setValue('community.example.invalid'); await wrapper.get('form').trigger('submit')
  wrapper.unmount()
  if (failure) reject(new Error('Native failure')); else resolve(discovered)
  await flushPromises()
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false)
})
it.each([false, true])('retires pending native context on unmount (native failure: %s)', async failure => {
  let resolve!: () => void, reject!: (value: unknown) => void
  calls.initialize.mockImplementation(() => new Promise<void>((r, j) => { resolve = r; reject = j }))
  const wrapper = mount(NativeBootstrap); wrapper.unmount()
  if (failure) reject(new Error('Native context retired')); else resolve()
  await flushPromises()
  expect(calls.connect).not.toHaveBeenCalled()
})

afterEach(() => vi.unstubAllGlobals())
it.each([false, true])('opens the normal web instance without native-preview discovery (failure: %s)', async failure => {
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('location', new URL('http://tauri.localhost/'))
  vi.stubGlobal('__MNEMA_WEB_DESKTOP__', true)
  if (failure) calls.open.mockRejectedValue('This instance needs an update for the desktop client.')
  else calls.open.mockResolvedValue(undefined)
  const wrapper = mount(NativeBootstrap); await flushPromises()
  wrapper.get('input').element.value = 'community.example.invalid'
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(calls.initialize).not.toHaveBeenCalled()
  expect(calls.connect).not.toHaveBeenCalled()
  expect(calls.open).toHaveBeenCalledExactlyOnceWith('desktop_open_instance', { address: 'community.example.invalid' })
  expect(wrapper.find('[role="alert"]').exists()).toBe(failure)
  if (failure) expect(wrapper.text()).toContain('This instance needs an update')
  expect(wrapper.find('[data-testid="native-community-app"]').exists()).toBe(false)
  wrapper.unmount()
})
