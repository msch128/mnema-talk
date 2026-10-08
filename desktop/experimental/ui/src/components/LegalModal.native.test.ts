import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
const calls = vi.hoisted(() => ({ api: vi.fn() }))
vi.mock('../lib/api', () => ({ api: calls.api }))
vi.mock('./BaseDialog.vue', () => ({ default: defineComponent({ template: '<div><slot name="badge"/><slot/></div>' }) }))
import LegalModal from './LegalModal.vue'
beforeEach(() => { calls.api.mockReset() })
it('does not display generic operator facts while native server metadata is pending', async () => {
  calls.api.mockReturnValue(new Promise(() => {}))
  const wrapper = mount(LegalModal)
  expect(wrapper.find('[role="status"]').exists()).toBe(true)
  expect(wrapper.text()).not.toContain('admin@example.com')
  expect(calls.api).toHaveBeenCalledWith('/api/legal', expect.objectContaining({ decode: expect.any(Function) }))
  wrapper.unmount()
})
it('shows a fixed unavailable state on native denial without claiming loaded operator data', async () => {
  calls.api.mockRejectedValue(new Error('synthetic-private-error'))
  const wrapper = mount(LegalModal); await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  expect(wrapper.text()).not.toContain('synthetic-private-error')
  expect(wrapper.text()).not.toContain('admin@example.com')
  wrapper.unmount()
})
it('renders actual verified-profile metadata after the shared decoder completes', async () => {
  calls.api.mockResolvedValue({ operator_name: 'Synthetic server operator', operator_email: 'fixture@example.invalid', operator_country: 'Test', project_notice: 'Synthetic', media_retention_days: 0, session_expiry_days: 30, stun_servers: [], update_check: false, legal_version: 'fixture' })
  const wrapper = mount(LegalModal); await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.find('[role="status"]').exists()).toBe(false)
  expect(wrapper.text()).toContain('Synthetic server operator')
  expect(wrapper.text()).toContain('fixture@example.invalid')
  wrapper.unmount()
})
