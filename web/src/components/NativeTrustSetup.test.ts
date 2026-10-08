import { mount, flushPromises } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { decodeTrustPreview, decodeTrustStatus } from '../lib/nativeTrust'
import type { NativeTrustPresentationPort } from '../lib/nativeTrust'
vi.mock('./BaseDialog.vue', () => ({ default: { template: '<section><slot /></section>' } }))
import NativeTrustSetup from './NativeTrustSetup.vue'
import { setLocale } from '../i18n'
const CHANNEL = '00000000-0000-4000-8000-000000000001'
const OTHER = '00000000-0000-4000-8000-000000000002'
const OPERATION = '00000000-0000-4000-8000-000000000003'
function preview() { return { version: 1, operation_id: OPERATION, operation_kind: 'first_root', expires_at: '2026-10-08T13:00:00Z', scope: { origin: 'https://community.example.invalid', community_id: 'own synthetic fixture', channel_id: CHANNEL, account_id: OTHER, device_id: OPERATION, group_id: 'Z3JvdXA=' }, root_fingerprint_hex: 'a'.repeat(64), root_public_key_hex: 'b'.repeat(64), device_public_key_hex: 'c'.repeat(64) } }
function harness() {
  let listener: (status: unknown) => void = () => {}
  const unsubscribe = vi.fn()
  const port: NativeTrustPresentationPort = { beginFirstRoot: vi.fn(async () => preview()), requestConfirmation: vi.fn(async () => ({ operation_id: OPERATION, state: 'pending' })), cancel: vi.fn(async () => {}), subscribe: vi.fn(callback => { listener = callback; return unsubscribe }) }
  return { port, unsubscribe, notice: (value: unknown) => listener(value) }
}
it('accepts only closed public preview/status DTOs and copies mutable wire scope', () => {
  const wire = preview(); const result = decodeTrustPreview(wire); wire.scope.origin = 'http://foreign.invalid'; expect(result.scope.origin).toBe('https://community.example.invalid')
  for (const value of [{ ...preview(), approved: true }, { ...preview(), private_key: 'synthetic' }, { ...preview(), root_public_key_hex: 'b'.repeat(63) }, { ...preview(), scope: { ...preview().scope, origin: 'https://community.example.invalid/path' } }, { ...preview(), scope: { ...preview().scope, group_id: 'Zh==' } }, { ...preview(), scope: { ...preview().scope, community_id: 'a\nspoof' } }]) expect(() => decodeTrustPreview(value)).toThrow()
  expect(() => decodeTrustStatus({ operation_id: OPERATION, state: 'root_saved', grant: true })).toThrow()
  expect(() => decodeTrustStatus({ operation_id: OPERATION, state: 'approved' })).toThrow()
})
it('shows the public scope and only requests the native dialog with its operation ID', async () => {
  const h = harness(); const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  await w.get('button').trigger('click'); await flushPromises(); expect(h.port.beginFirstRoot).toHaveBeenCalledExactlyOnceWith(CHANNEL)
  expect(w.text()).toContain(preview().root_fingerprint_hex); expect(w.text()).toContain(preview().scope.origin)
  await w.get('button').trigger('click'); await flushPromises(); expect(h.port.requestConfirmation).toHaveBeenCalledExactlyOnceWith(OPERATION)
  h.notice({ operation_id: OPERATION, state: 'root_saved' }); await flushPromises(); expect(w.text()).toContain('Der Schlüssel wurde gespeichert.')
  h.notice({ operation_id: OPERATION, state: 'pending' }); await flushPromises(); expect(w.text()).toContain('Der Schlüssel wurde gespeichert.')
  w.unmount(); expect(h.unsubscribe).toHaveBeenCalled()
})
it('cancels a delayed prepared operation after unmount and never asks for confirmation', async () => {
  const h = harness(); let resolve!: (value: unknown) => void
  h.port.beginFirstRoot = vi.fn(() => new Promise(r => { resolve = r }))
  const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } }); await w.get('button').trigger('click'); w.unmount()
  resolve(preview()); await flushPromises(); expect(h.port.cancel).toHaveBeenCalledExactlyOnceWith(OPERATION); expect(h.port.requestConfirmation).not.toHaveBeenCalled()
})
it('retires a prepared operation on channel replacement and ignores its late status', async () => {
  const h = harness(); const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } }); await w.get('button').trigger('click'); await flushPromises()
  await w.setProps({ channelId: OTHER }); expect(h.port.cancel).toHaveBeenCalledWith(OPERATION)
  h.notice({ operation_id: OPERATION, state: 'root_saved' }); await flushPromises(); expect(w.text()).not.toContain('Der Schlüssel wurde gespeichert.')
  w.unmount()
})
it('cancels a wrong-channel native preview and hides native error details', async () => {
  const h = harness(); h.port.beginFirstRoot = vi.fn(async () => ({ ...preview(), scope: { ...preview().scope, channel_id: OTHER } }))
  const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } }); await w.get('button').trigger('click'); await flushPromises()
  expect(h.port.cancel).toHaveBeenCalledWith(OPERATION); expect(w.text()).not.toContain(preview().root_fingerprint_hex); expect(w.get('[role="alert"]').text()).toBe('Die Einrichtung konnte nicht gestartet werden.')
  w.unmount()
})

it('uses the selected app language for native setup presentation', async () => {
  setLocale('en')
  const h = harness(); const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  try { expect(w.text()).toContain('Prepare setup'); await w.get('button').trigger('click'); await flushPromises(); expect(w.text()).toContain('Check in security dialog') }
  finally { w.unmount(); setLocale('de') }
})

it.each(['cancelled', 'expired', 'failed'])('shows native %s and permits a new explicit preparation', async state => {
  const h = harness(), w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  h.notice({ operation_id: OPERATION, state: 'pending' })
  await w.get('button').trigger('click'); await flushPromises()
  h.notice({ operation_id: OTHER, state: 'root_saved' }); await flushPromises()
  expect(w.find('[role="status"]').exists()).toBe(false)
  h.notice({ operation_id: OPERATION, state }); await flushPromises()
  expect(w.find('[role="status"]').exists()).toBe(true)
  expect(w.find('dl').exists()).toBe(false)
  await w.get('button').trigger('click'); await flushPromises()
  expect(h.port.beginFirstRoot).toHaveBeenCalledTimes(2)
  w.unmount()
})
it('ignores a retired subscription after replacing its captured port and closes the current preparation', async () => {
  const h = harness(), next = harness(), w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  await w.get('button').trigger('click'); await flushPromises()
  await w.setProps({ port: next.port })
  h.notice({ operation_id: OPERATION, state: 'root_saved' }); await flushPromises()
  expect(w.emitted('status')).toBeUndefined()
  await w.get('button').trigger('click'); await flushPromises()
  await w.findAll('button').at(-1)!.trigger('click'); await flushPromises()
  expect(next.port.cancel).toHaveBeenCalledWith(OPERATION)
  expect(w.emitted('close')).toHaveLength(1)
  w.unmount()
})
it('rejects malformed status without leaking native details or granting success', async () => {
  const h = harness(), w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  await w.get('button').trigger('click'); await flushPromises()
  h.notice({ operation_id: OPERATION, state: 'approved', private_details: 'synthetic-detail' }); await flushPromises()
  expect(w.find('[role="alert"]').exists()).toBe(true)
  expect(w.text()).not.toContain('synthetic-detail')
  expect(w.emitted('status')).toBeUndefined()
  w.unmount()
})
it.each(['cancelled', 'invalid', 'failed'])('handles %s OS confirmation acknowledgement', async mode => {
  const h = harness()
  h.port.requestConfirmation = vi.fn(async () => {
    if (mode === 'failed') throw new Error('private synthetic native detail')
    return { operation_id: mode === 'invalid' ? OTHER : OPERATION, state: mode === 'cancelled' ? 'cancelled' : 'pending' }
  })
  const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  await w.get('button').trigger('click'); await flushPromises()
  await w.get('button').trigger('click'); await flushPromises()
  expect(w.find('[role="alert"]').exists()).toBe(mode !== 'cancelled')
  expect(w.find('dl').exists()).toBe(mode !== 'cancelled')
  expect(w.text()).not.toContain('private synthetic native detail')
  w.unmount()
})
it('preserves terminal OS status over late pending ACK and drops a confirmation after unmount', async () => {
  for (const retired of [false, true]) {
    const h = harness(); let resolve!: (value: unknown) => void
    h.port.requestConfirmation = vi.fn(() => new Promise(r => { resolve = r }))
    const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
    await w.get('button').trigger('click'); await flushPromises()
    await w.get('button').trigger('click')
    if (retired) w.unmount(); else { h.notice({ operation_id: OPERATION, state: 'root_saved' }); await flushPromises() }
    resolve({ operation_id: OPERATION, state: 'pending' }); await flushPromises()
    if (!retired) { expect(w.text()).toContain('Der Schlüssel wurde gespeichert.'); w.unmount() }
  }
})
it('suppresses duplicate preparation while waiting and never prepares an invalid channel', async () => {
  const h = harness(); let resolve!: (value: unknown) => void
  h.port.beginFirstRoot = vi.fn(() => new Promise(r => { resolve = r }))
  const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
  await w.get('button').trigger('click'); await w.get('button').trigger('click')
  expect(h.port.beginFirstRoot).toHaveBeenCalledOnce()
  resolve(preview()); await flushPromises()
  await w.setProps({ channelId: 'invalid' })
  expect(w.get('button').attributes('disabled')).toBeDefined()
  w.unmount()
})
it('handles failed cancellation when retiring, receiving a late preview, or rejecting wrong scope', async () => {
  for (const mode of ['prepared', 'late', 'wrong'] as const) {
    const h = harness(); h.port.cancel = vi.fn(async () => { throw new Error('Native cleanup unavailable') })
    let resolve!: (value: unknown) => void
    if (mode === 'late') h.port.beginFirstRoot = vi.fn(() => new Promise(r => { resolve = r }))
    if (mode === 'wrong') h.port.beginFirstRoot = vi.fn(async () => ({ ...preview(), scope: { ...preview().scope, channel_id: OTHER } }))
    const w = mount(NativeTrustSetup, { props: { channelId: CHANNEL, port: h.port } })
    await w.get('button').trigger('click'); await flushPromises(); w.unmount()
    if (mode === 'late') resolve(preview())
    await flushPromises()
    expect(h.port.cancel).toHaveBeenCalledWith(OPERATION)
  }
})
