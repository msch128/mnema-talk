import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminOnboardingTab from './AdminOnboardingTab.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useServerFeaturesStore } from '../stores/serverFeatures'

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
      json: { community_name: 'Home', welcome_text: 'Hi', rules_text: 'Be kinder', start_channel_id: CH_TEXT, require_reacceptance: true, session_inactivity_days: 7 }
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
    expect(useToastStore().toasts.length).toBe(2)
    expect(ok.find<HTMLInputElement>('#onb-name').element.value).toBe('Changed')
  })

  it('refuses over-long texts', async () => {
    const w = await mountTab()
    await w.find('#onb-name').setValue('x'.repeat(61))
    expect(w.find('[role="alert"]').exists()).toBe(true)
    expect(w.find('[data-testid="onboarding-save"]').attributes('disabled')).toBeDefined()
  })

  it('sets the inactivity sign-out and refuses days outside 1 to 365', async () => {
    const w = await mountTab()
    expect(w.find<HTMLInputElement>('[data-testid="inactivity-days"]').element.value).toBe('7')
    for (const bad of ['0', '366']) {
      await w.find('[data-testid="inactivity-days"]').setValue(bad)
      expect(w.find('[data-testid="onboarding-save"]').attributes('disabled')).toBeDefined()
      expect(w.find('[role="alert"]').text()).toContain('1 to 365')
    }
    await w.find('[data-testid="inactivity-days"]').setValue('30')
    apiMock.mockResolvedValue({ ...settings, session_inactivity_days: 30 })
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/onboarding', expect.objectContaining({
      json: expect.objectContaining({ session_inactivity_days: 30 })
    }))
    expect(w.find<HTMLInputElement>('[data-testid="inactivity-days"]').element.value).toBe('30')
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

  describe('community icon', () => {
    async function choose(w: Awaited<ReturnType<typeof mountTab>>, file: File) {
      const input = w.get<HTMLInputElement>('[data-testid="community-icon-input"]')
      Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
      await input.trigger('change')
      await flushPromises()
    }

    it('offers an upload while there is none and previews the stored icon otherwise', async () => {
      let w = await mountTab()
      expect(w.find('[data-testid="community-icon-preview"]').exists()).toBe(false)
      expect(w.find('[data-testid="community-icon-remove"]').exists()).toBe(false)
      expect(w.get('[data-testid="community-icon-change"]').text()).toBe('Upload icon')

      w = await mountTab({ ...settings, community_icon: 'v1' })
      expect(w.get('[data-testid="community-icon-preview"]').attributes('src')).toBe('/api/community/icon?size=128&v=v1')
      expect(w.get('[data-testid="community-icon-change"]').text()).toBe('Change icon')
      expect(w.find('[data-testid="community-icon-remove"]').exists()).toBe(true)
    })

    it('uploads the picture as multipart without touching unsaved text', async () => {
      const w = await mountTab()
      await w.find('#onb-name').setValue('Unsaved')
      apiMock.mockResolvedValue({ ...settings, community_icon: 'v2' })
      await choose(w, new File(['png'], 'logo.png', { type: 'image/png' }))
      const [path, options] = apiMock.mock.lastCall as [string, ApiOptions]
      expect(path).toBe('/api/admin/community/icon')
      expect(options.method).toBe('PUT')
      expect((options.form as FormData).get('icon')).toBeInstanceOf(File)
      expect(w.get('[data-testid="community-icon-preview"]').attributes('src')).toBe('/api/community/icon?size=128&v=v2')
      expect(w.find<HTMLInputElement>('#onb-name').element.value).toBe('Unsaved')
      expect(useServerFeaturesStore().communityIcon).toBe('v2')
      expect(useToastStore().toasts.length).toBe(1)
    })

    it('refuses a picture over the limit before any request', async () => {
      const w = await mountTab()
      apiMock.mockClear()
      await choose(w, new File([new Uint8Array(6 * 1024 * 1024)], 'big.png', { type: 'image/png' }))
      expect(apiMock).not.toHaveBeenCalled()
      expect(useToastStore().toasts[0]?.text).toContain('5 MB')
    })

    it('reports a refused upload and keeps the icon', async () => {
      const w = await mountTab({ ...settings, community_icon: 'v1' })
      apiMock.mockRejectedValue(new Error('rejected'))
      await choose(w, new File(['x'], 'logo.png', { type: 'image/png' }))
      expect(w.get('[data-testid="community-icon-preview"]').attributes('src')).toBe('/api/community/icon?size=128&v=v1')
      expect(useToastStore().toasts.length).toBe(1)
    })

    it('removes the icon', async () => {
      const w = await mountTab({ ...settings, community_icon: 'v1' })
      useServerFeaturesStore().setCommunityIcon('v1')
      apiMock.mockResolvedValue({ ...settings })
      await w.get('[data-testid="community-icon-remove"]').trigger('click')
      await flushPromises()
      expect(apiMock).toHaveBeenLastCalledWith('/api/admin/community/icon', { method: 'DELETE', decode: expect.any(Function) })
      expect(w.find('[data-testid="community-icon-preview"]').exists()).toBe(false)
      expect(useServerFeaturesStore().communityIcon).toBe('')
    })
  })
})
