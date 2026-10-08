import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { setLocale } from '../i18n'
import LegalModal from './LegalModal.vue'
import BaseDialog from './BaseDialog.vue'
import type { ServerLegal } from '../types/rest'
const legal: ServerLegal = { legal_version: '2.0', media_retention_days: 7, operator_country: 'Synthetic country', operator_email: 'operator@example.com', operator_name: 'Synthetic operator', project_notice: 'Synthetic notice', session_expiry_days: 10, stun_servers: ['stun:stun.example.com'], turn_servers: [], update_check: true }
let wrapper: ReturnType<typeof mount<typeof LegalModal>>
beforeEach(() => { setLocale('en') })
afterEach(() => { wrapper?.unmount(); vi.unstubAllGlobals(); setLocale('de') })
describe('LegalModal', () => {
  it('renders validated server policy and each document tab', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(legal))))
    wrapper = mount(LegalModal)
    await flushPromises()
    expect(wrapper.text()).toContain('Synthetic operator')
    expect(wrapper.text()).toContain('stun:stun.example.com')
    expect(wrapper.text()).toContain('2.0')
    expect(wrapper.find('[data-testid="legal-update-check"]').exists()).toBe(true)
    expect(wrapper.findAll('section')).toHaveLength(5)
    const tabs = wrapper.findAll('button[aria-pressed]')
    for (const [index, sections] of [[1, 1], [2, 2], [3, 1], [4, 1], [0, 5]] as const) {
      await tabs[index]!.trigger('click')
      expect(tabs[index]!.attributes('aria-pressed')).toBe('true')
      expect(wrapper.findAll('section')).toHaveLength(sections)
    }
    await wrapper.findAll('button').at(-1)!.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    expect(wrapper.emitted('close')).toHaveLength(2)
  })
  it.each(['blank', 'unavailable', 'invalid', 'rejected'] as const)('keeps default policy when the response is %s', async mode => {
    const fetcher = vi.fn<typeof fetch>()
    if (mode === 'rejected') fetcher.mockRejectedValue(new Error('synthetic offline'))
    else fetcher.mockResolvedValue(new Response(JSON.stringify(mode === 'blank' ? { ...legal, legal_version: '', media_retention_days: 0, session_expiry_days: 0, operator_name: '', operator_email: '', operator_country: '', project_notice: '', stun_servers: [], update_check: false } : {}), { status: mode === 'unavailable' ? 503 : 200 }))
    vi.stubGlobal('fetch', fetcher)
    wrapper = mount(LegalModal)
    await flushPromises()
    expect(wrapper.text()).toContain('admin@example.com')
    expect(wrapper.text()).toContain('1.4')
    expect(wrapper.find('[data-testid="legal-update-check"]').exists()).toBe(false)
  })
})
