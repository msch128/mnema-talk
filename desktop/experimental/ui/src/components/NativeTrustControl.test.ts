import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { initializeNativeContext, installNativePort } from '../lib/nativeTransport'
import type { NativePort } from '../lib/nativeTransport'
import NativeTrustControl from './NativeTrustControl.vue'
vi.mock('./BaseDialog.vue', () => ({ default: { template: '<section><slot /></section>' } }))
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const CHANNEL = '00000000-0000-4000-8000-000000000002'
const OPERATION = '00000000-0000-4000-8000-000000000003'
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
beforeEach(async () => {
  invoke = vi.fn(async command => command === 'native_context' ? { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: CONTEXT } : { context: CONTEXT, status: 200, body: command === 'native_trust_begin_first_root' ? { version: 1, operation_id: OPERATION, operation_kind: 'first_root', expires_at: '2026-10-08T16:00:00Z', scope: { origin: 'https://example.invalid', community_id: 'synthetic-community', channel_id: CHANNEL, account_id: CONTEXT, device_id: OPERATION, group_id: 'YWJj' }, root_fingerprint_hex: 'a'.repeat(64), root_public_key_hex: 'b'.repeat(64), device_public_key_hex: 'c'.repeat(64) } : { operation_id: OPERATION, state: 'cancelled' } })
  installNativePort({ invoke, channel: () => ({ onmessage() {} }), trustChannel: () => ({ onmessage() {} }) }); await initializeNativeContext()
})
it('mounts the actual compact trust component without preparing or approving on open, cancels on unmount', async () => {
  const wrapper = mount(NativeTrustControl, { props: { channelId: CHANNEL } })
  await wrapper.get('button').trigger('click'); await flushPromises()
  expect(invoke.mock.calls.map(c => c[0])).toEqual(['native_context'])
  const prepare = wrapper.findAll('button')[1]; expect(prepare).toBeDefined(); await prepare!.trigger('click'); await flushPromises()
  expect(invoke).toHaveBeenLastCalledWith('native_trust_begin_first_root', expect.objectContaining({ context: CONTEXT, channelId: CHANNEL }))
  expect(wrapper.text()).toContain('a'.repeat(64))
  wrapper.unmount(); await flushPromises()
  expect(invoke).toHaveBeenLastCalledWith('native_trust_cancel', { context: CONTEXT, operationId: OPERATION, profileIntent: CONTEXT })
  expect(invoke.mock.calls.some(c => c[0] === 'native_trust_request_confirmation')).toBe(false)
})
it('shows a bounded generic error when the captured native presentation is unavailable', async () => {
  installNativePort({ invoke, channel: () => ({ onmessage() {} }) }); await initializeNativeContext()
  const wrapper = mount(NativeTrustControl, { props: { channelId: CHANNEL } })
  await wrapper.get('button').trigger('click')
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  expect(wrapper.findAll('button')).toHaveLength(1)
  expect(invoke.mock.calls.some(c => c[0] === 'native_trust_begin_first_root')).toBe(false)
  wrapper.unmount()
})
