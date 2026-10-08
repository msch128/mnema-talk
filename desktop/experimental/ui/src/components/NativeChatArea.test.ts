import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, expect, it, vi } from 'vitest'
import { initializeNativeContext, installNativePort } from '../lib/nativeTransport'
import type { NativePort } from '../lib/nativeTransport'
import type { StatusChannel } from '../lib/trustPort'
import NativeChatArea from './NativeChatArea.vue'
import { setLocale } from '../i18n'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const CHANNEL = '00000000-0000-4000-8000-000000000002'
const OTHER = '00000000-0000-4000-8000-000000000003'
let channel: StatusChannel
let invoke: ReturnType<typeof vi.fn<NativePort['invoke']>>
function row(event: string, body: string) { return { id: event, number: 1, channel_id: CHANNEL, client_event_id: event, account_id: CONTEXT, device_id: OTHER, body } }
const ack = { context: CONTEXT, status: 200, body: { state: 'completed' } }
beforeEach(async () => {
  setLocale('de'); channel = { onmessage() {} }
  invoke = vi.fn(async command => {
    if (command === 'native_context') return { context: CONTEXT, content_authorization: 'unavailable', remembered_login: false, profile_intent: CONTEXT }
    channel.onmessage([]); return ack
  })
  installNativePort({ invoke, channel: () => ({ onmessage() {} }), chatChannel: () => { channel = { onmessage() {} }; return channel } }); await initializeNativeContext()
})
function mountChat() { return mount(NativeChatArea, { props: { channelId: CHANNEL, accountId: CONTEXT, channelName: 'synthetic', reload: 0 }, global: { plugins: [createPinia()] } }) }
it('renders verified native messages only after completed ACK and treats message markup as text', async () => {
  const wrapper = mountChat(); await flushPromises()
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce((_command, args) => new Promise(resolve => { finish = resolve; channel.onmessage([row(String(args?.clientEventId), String(args?.body))]) }))
  await wrapper.get('textarea').setValue('<img src="https://external.invalid/tracker">')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.find('article').exists()).toBe(false)
  finish(ack); await flushPromises()
  expect(wrapper.get('article').text()).toContain('<img src='); expect(wrapper.find('img').exists()).toBe(false)
  expect(wrapper.get('textarea').element.value).toBe(''); wrapper.unmount()
})
it('keeps the same explicit event and body for retry after lost acknowledgement without auto replay', async () => {
  const wrapper = mountChat(); await flushPromises()
  invoke.mockRejectedValueOnce(new Error('synthetic lost ack'))
  await wrapper.get('textarea').setValue('  same synthetic message\n'); await wrapper.get('form').trigger('submit'); await flushPromises()
  const first = invoke.mock.calls.find(call => call[0] === 'native_chat_publish'); expect(first).toBeDefined()
  expect(wrapper.find('article').exists()).toBe(false); expect(wrapper.get('textarea').attributes('disabled')).toBeDefined()
  invoke.mockImplementationOnce(async (_command, args) => { channel.onmessage([row(String(args?.clientEventId), String(args?.body))]); return ack })
  await wrapper.get('form').trigger('submit'); await flushPromises()
  const calls = invoke.mock.calls.filter(call => call[0] === 'native_chat_publish')
  expect(calls).toHaveLength(2); expect(calls[1]?.[1]?.clientEventId).toBe(first?.[1]?.clientEventId); expect(calls[1]?.[1]?.body).toBe(first?.[1]?.body)
  expect(wrapper.findAll('article')).toHaveLength(1); wrapper.unmount()
})
it('disposes the old presentation on channel replacement and rejects its late messages', async () => {
  let finish!: (value: unknown) => void
  invoke.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  const wrapper = mountChat(); const oldChannel = channel
  await wrapper.setProps({ channelId: OTHER }); await flushPromises()
  oldChannel.onmessage([row(OTHER, 'old channel late message')]); finish(ack); await flushPromises()
  expect(wrapper.text()).not.toContain('old channel late message'); wrapper.unmount()
})
it('rejects oversized or invalid Unicode before pending admission and keeps the draft editable', async () => {
  const wrapper = mountChat(); await flushPromises()
  for (const body of ['🌲'.repeat(6144) + 'x', '\ud800', '\udc00', '\ud800x']) {
    await wrapper.get('textarea').setValue(body); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('textarea').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('textarea').element.value).toBe(body)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(invoke.mock.calls.some(call => call[0] === 'native_chat_publish')).toBe(false)
  }
  invoke.mockImplementationOnce(async (_command, args) => { channel.onmessage([row(String(args?.clientEventId), String(args?.body))]); return ack })
  await wrapper.get('textarea').setValue('🌲'.repeat(6144)); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.findAll('article')).toHaveLength(1); wrapper.unmount()
})
it('rehydrates verified session history on returning to a channel without rewinding native receive cursor', async () => {
  const wrapper = mountChat(); await flushPromises()
  invoke.mockImplementationOnce(async (_command, args) => { channel.onmessage([row(String(args?.clientEventId), String(args?.body))]); return ack })
  await wrapper.get('textarea').setValue('cached actual projection'); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.text()).toContain('cached actual projection')
  await wrapper.setProps({ channelId: OTHER }); await flushPromises(); expect(wrapper.text()).not.toContain('cached actual projection')
  await wrapper.setProps({ channelId: CHANNEL }); await flushPromises(); expect(wrapper.text()).toContain('cached actual projection')
  expect(invoke.mock.calls.filter(call => call[0] === 'native_chat_receive').every(call => !Object.hasOwn(call[1] ?? {}, 'cursor'))).toBe(true)
  wrapper.unmount()
})
