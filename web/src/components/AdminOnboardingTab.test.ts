import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminOnboardingTab from './AdminOnboardingTab.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const CH_TEXT = '00000000-0000-4000-8000-0000000000a1'
const CH_VOICE = '00000000-0000-4000-8000-0000000000a2'
const settings = { community_name: 'Home', welcome_text: 'Hi', rules_text: '', rules_version: 0, start_channel_id: null, session_inactivity_days: 7 }
const stubs = { MarkdownContent: { props: ['content'], template: '<div class="md">{{ content }}</div>' } }

async function mountTab(initial: Record<string, unknown> = settings) {
  apiMock.mockResolvedValue(initial)
  const chat = useChatStore()
  chat.categories = [{ id: 'cat', name: 'Main', sort_order: 0, channels: [
    { id: CH_TEXT, name: 'general', type: 'text', topic: '', sort_order: 0, category_id: 'cat', user_limit: 0 },
    { id: CH_VOICE, name: 'Lounge', type: 'voice', topic: '', sort_order: 1, category_id: 'cat', user_limit: 0 }
  ] }] as never
  const w = mount(AdminOnboardingTab, { global: { stubs } })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminOnboardingTab', () => {
  it('loads the settings and lists only text channels as start channels', async () => {
    const w = await mountTab()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/onboarding', { decode: expect.any(Function) })
    expect(w.find<HTMLInputElement>('#onb-name').element.value).toBe('Home')
    const options = w.findAll('#onb-start option').map(o => o.text())
    expect(options).toEqual(['The first channel a member can see', '# general'])
  })

  it('saves everything in one request and clears the re-acceptance box', async () => {
    const w = await mountTab({ ...settings, rules_text: 'Be kind', rules_version: 1 })
    expect(w.find('[data-testid="onboarding-save"]').attributes('disabled')).toBeDefined()
    await w.find('#onb-rules').setValue('Be kinder')
    await w.find('[data-testid="reaccept"]').setValue(true)
    await w.find('#onb-start').setValue(CH_TEXT)
    apiMock.mockResolvedValue({ ...settings, rules_text: 'Be kinder', rules_version: 2, start_channel_id: CH_TEXT })
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/onboarding', {
      method: 'PUT', decode: expect.any(Function),
      json: { community_name: 'Home', welcome_text: 'Hi', rules_text: 'Be kinder', start_channel_id: CH_TEXT, require_reacceptance: true }
    })
    expect(useToastStore().toasts.length).toBe(1)
    expect(w.find('[data-testid="reaccept"]').exists()).toBe(true)
    expect(w.find<HTMLInputElement>('[data-testid="reaccept"]').element.checked).toBe(false)
  })

  it('only offers re-acceptance when rules exist and never sends it for empty rules', async () => {
    const w = await mountTab()
    expect(w.find('[data-testid="reaccept"]').exists()).toBe(false)
    await w.find('#onb-name').setValue('Other')
    apiMock.mockResolvedValue({ ...settings, community_name: 'Other' })
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/onboarding', expect.objectContaining({
      json: expect.objectContaining({ rules_text: '', start_channel_id: null, require_reacceptance: false })
    }))
  })

  it('previews Markdown instead of editing and toggles back', async () => {
    const w = await mountTab()
    await w.find('#onb-rules').setValue('# Rules')
    const toggles = w.findAll('button[aria-pressed]')
    await toggles[1]!.trigger('click')
    expect(w.find('[data-testid="rules-preview"]').text()).toBe('# Rules')
    expect(w.find('#onb-rules').exists()).toBe(false)
    await toggles[1]!.trigger('click')
    expect(w.find('#onb-rules').exists()).toBe(true)
    await toggles[0]!.trigger('click')
    expect(w.find('[data-testid="welcome-preview"]').exists()).toBe(true)
  })

  it('reports a failed load and a failed save', async () => {
    apiMock.mockRejectedValue(new Error('down'))
    const w = mount(AdminOnboardingTab, { global: { stubs } })
    await flushPromises()
    expect(w.find('form').exists()).toBe(false)
    expect(useToastStore().toasts.length).toBe(1)

    const ok = await mountTab()
    await ok.find('#onb-name').setValue('Changed')
    apiMock.mockRejectedValue(new Error('nope'))
    await ok.find('form').trigger('submit')
    await flushPromises()
    // A failed save is shown inline, the draft stays as typed and can be saved again.
    expect(useToastStore().toasts.length).toBe(1)
    expect(ok.find('[data-testid="unsaved-error"]').text()).toBe('nope')
    expect(ok.find<HTMLInputElement>('#onb-name').element.value).toBe('Changed')
    expect(ok.find('[data-testid="onboarding-save"]').attributes('disabled')).toBeUndefined()
  })

  it('refuses over-long texts', async () => {
    const w = await mountTab()
    await w.find('#onb-name').setValue('x'.repeat(61))
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.find('[data-testid="onboarding-save"]').attributes('disabled')).toBeDefined()
  })

  it('has a heading, leaves DMs and sign-out to the System page and sends neither', async () => {
    const w = await mountTab()
    expect(w.get('h3').text()).toBe('Welcome & rules')
    expect(w.find('[data-testid="dm-switch"]').exists()).toBe(false)
    expect(w.find('[data-testid="inactivity-setting"]').exists()).toBe(false)
    expect(w.get<HTMLInputElement>('#onb-name').attributes('placeholder')).toBe(window.location.hostname)
    await w.get('#onb-name').setValue('Home 2')
    apiMock.mockResolvedValue({ ...settings, community_name: 'Home 2' })
    await w.get('form').trigger('submit')
    await flushPromises()
    const body = (apiMock.mock.lastCall![1] as { json: Record<string, unknown> }).json
    expect(body).not.toHaveProperty('session_inactivity_days')
    expect(body).not.toHaveProperty('direct_messages_enabled')
  })

  it('offers a retry when the first load fails', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const w = mount(AdminOnboardingTab, { global: { stubs } })
    await flushPromises()
    expect(w.find('[data-testid="onboarding-load-failed"]').exists()).toBe(true)
    apiMock.mockResolvedValue(settings)
    await w.get('[data-testid="onboarding-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="onboarding-settings"]').exists()).toBe(true)
    expect(w.find('[data-testid="onboarding-load-failed"]').exists()).toBe(false)
  })

  it('does not report a saved change as failed when the shared copy cannot refresh', async () => {
    const w = await mountTab()
    await w.find('#onb-name').setValue('Renamed')
    apiMock.mockResolvedValueOnce({ ...settings, community_name: 'Renamed' })
    apiMock.mockRejectedValue(new Error('offline'))
    await w.find('form').trigger('submit')
    await flushPromises()
    const texts = useToastStore().toasts.map(x => x.text)
    expect(texts).toContain('Welcome and rules saved.')
    expect(useToastStore().toasts.some(x => x.type === 'error')).toBe(false)
  })

  it('counts only real differences as changes', async () => {
    const w = await mountTab({ ...settings, rules_text: 'Be kind', rules_version: 1 })
    const saveOff = () => w.find('[data-testid="onboarding-save"]').attributes('disabled') !== undefined
    await w.find('#onb-name').setValue('  Home ')
    expect(saveOff()).toBe(true)
    await w.find('#onb-welcome').setValue('Hi there')
    expect(saveOff()).toBe(false)
    await w.find('#onb-welcome').setValue('Hi')
    expect(saveOff()).toBe(true)
    await w.find('[data-testid="reaccept"]').setValue(true)
    expect(saveOff()).toBe(false)
    await w.find('[data-testid="unsaved-reset"]').trigger('click')
    expect(saveOff()).toBe(true)
    expect(w.find<HTMLInputElement>('[data-testid="reaccept"]').element.checked).toBe(false)
  })

  it('stores nothing before Save and asks before the draft is dropped', async () => {
    const w = await mountTab()
    apiMock.mockClear()
    await w.find('#onb-name').setValue('Elsewhere')
    await w.find('#onb-start').setValue(CH_TEXT)
    expect(apiMock).not.toHaveBeenCalled()
    expect(w.find('[data-testid="unsaved-status"]').text()).toBe('You have unsaved changes')
  })
})
