import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
const calls = vi.hoisted(() => ({ initialize: vi.fn(), connect: vi.fn(), disconnect: vi.fn() }))
vi.mock('./lib/nativeTransport', () => ({ initializeNativeContext: calls.initialize, connectNative: calls.connect, disconnectNative: calls.disconnect }))
vi.mock('./App.vue', () => ({ default: { template: '<div data-testid="native-community-app">Community</div>' } }))
vi.mock('./stores/chat', () => ({ useChatStore: () => ({ closeWebSocket: vi.fn() }) }))
vi.mock('./stores/voice', () => ({ useVoiceStore: () => ({ disconnect: vi.fn() }) }))
vi.mock('./stores/auth', () => ({ useAuthStore: () => ({ user: null }) }))
import NativeBootstrap from './NativeBootstrap.vue'
beforeEach(() => { vi.clearAllMocks(); calls.initialize.mockResolvedValue(undefined); calls.disconnect.mockResolvedValue(undefined) })
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
