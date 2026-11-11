import type { ApiOptions } from '../../shared/lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminVanityLink from './AdminVanityLink.vue'
import { setLocale } from '../../i18n'
import { useToastStore } from '../../stores/toast'
import { useAuthStore } from '../../stores/auth'
import { userFixture } from '../../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

async function mountLink(initial = { slug: '', accepts_signups: false, path: '' }, role: 'admin' | 'user' = 'admin') {
  useAuthStore().user = userFixture({ role })
  apiMock.mockResolvedValue(initial)
  const w = mount(AdminVanityLink)
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminVanityLink', () => {
  it('starts empty with sign-ups off, no link to copy and Save off', async () => {
    const w = await mountLink()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/vanity-link', { decode: expect.any(Function) })
    expect(w.find<HTMLInputElement>('[data-testid="vanity-slug"]').element.value).toBe('')
    expect(w.find<HTMLInputElement>('[data-testid="vanity-accepts"]').element.disabled).toBe(true)
    expect(w.find('[data-testid="vanity-full"]').exists()).toBe(false)
    expect(w.get('[data-testid="vanity-save"]').attributes('disabled')).toBeDefined()
  })

  it('saves the slug and the switch together and shows the full link', async () => {
    const w = await mountLink()
    await w.get('[data-testid="vanity-slug"]').setValue('Mein-Clan')
    await w.get('[data-testid="vanity-accepts"]').setValue(true)
    apiMock.mockResolvedValue({ slug: 'mein-clan', accepts_signups: true, path: '/join/mein-clan' })
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/vanity-link', { method: 'PUT', json: { slug: 'Mein-Clan', accepts_signups: true }, decode: expect.any(Function) })
    expect(w.get('[data-testid="vanity-url"]').text()).toBe(`${window.location.origin}/join/mein-clan`)
    expect(w.get<HTMLInputElement>('[data-testid="vanity-slug"]').element.value).toBe('mein-clan')
    expect(useToastStore().toasts[0]?.text).toBe('Vanity link saved.')
  })

  it('refuses a slug that is too short before asking the server, and reports a refusal', async () => {
    const w = await mountLink()
    await w.get('[data-testid="vanity-slug"]').setValue('ab')
    expect(w.get('[data-testid="vanity-save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="vanity-slug"]').setValue('admin')
    apiMock.mockRejectedValue(new Error('This link name is reserved.'))
    await w.get('form').trigger('submit')
    await flushPromises()
    // The refusal is said inline in the bar, next to the form.
    expect(w.get('[data-testid="unsaved-error"]').text()).toBe('This link name is reserved.')
  })

  it('shows what is unsaved, takes it back on Reset and saves with Ctrl+S', async () => {
    const w = await mountLink({ slug: 'owls', accepts_signups: false, path: '/join/owls' })
    expect(w.find('[data-testid="unsaved-reset"]').exists()).toBe(false)
    await w.get('[data-testid="vanity-slug"]').setValue('hawks')
    expect(w.get('[data-testid="unsaved-status"]').text()).toBe('You have unsaved changes')
    await w.get('[data-testid="unsaved-reset"]').trigger('click')
    expect(w.get<HTMLInputElement>('[data-testid="vanity-slug"]').element.value).toBe('owls')
    // Only the case and spaces differ: nothing to save.
    await w.get('[data-testid="vanity-slug"]').setValue(' OWLS ')
    expect(w.get('[data-testid="vanity-save"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="vanity-slug"]').setValue('hawks')
    apiMock.mockResolvedValue({ slug: 'hawks', accepts_signups: false, path: '/join/hawks' })
    await w.get('form').trigger('keydown', { key: 's', ctrlKey: true })
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/vanity-link', expect.objectContaining({ method: 'PUT' }))
    expect(w.find('[data-testid="unsaved-reset"]').exists()).toBe(false)
  })

  it('removes the link by saving an empty name', async () => {
    const w = await mountLink({ slug: 'owls', accepts_signups: true, path: '/join/owls' })
    await w.get('[data-testid="vanity-slug"]').setValue('')
    apiMock.mockResolvedValue({ slug: '', accepts_signups: false, path: '' })
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/vanity-link', { method: 'PUT', json: { slug: '', accepts_signups: true }, decode: expect.any(Function) })
    expect(w.find('[data-testid="vanity-full"]').exists()).toBe(false)
    expect(useToastStore().toasts[0]?.text).toBe('Vanity link removed.')
  })

  it('copies the full link', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: write } })
    const w = await mountLink({ slug: 'owls', accepts_signups: false, path: '/join/owls' })
    await w.get('[data-testid="vanity-copy"]').trigger('click')
    await flushPromises()
    expect(write).toHaveBeenCalledWith(`${window.location.origin}/join/owls`)
    expect(w.get('[data-testid="vanity-copy"]').text()).toBe('Copied')
    write.mockRejectedValue(new Error('denied'))
    await w.get('[data-testid="vanity-copy"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(x => x.type === 'error')).toBe(true)
  })

  it('shows nothing until the setting is known', () => {
    apiMock.mockReturnValue(new Promise(() => {}))
    expect(mount(AdminVanityLink).find('[data-testid="vanity-link"]').exists()).toBe(false)
  })

  describe('for an Administrator who is not the Owner', () => {
    it('hides the switch while sign-ups are off and says why', async () => {
      const w = await mountLink({ slug: 'owls', accepts_signups: false, path: '/join/owls' }, 'user')
      expect(w.find('[data-testid="vanity-accepts"]').exists()).toBe(false)
      expect(w.get('[data-testid="vanity-owner-note"]').text()).toContain('Only the Owner')
    })

    it('shows the switch while sign-ups are on so they can be switched off, and a rename switches them off', async () => {
      const w = await mountLink({ slug: 'owls', accepts_signups: true, path: '/join/owls' }, 'user')
      expect(w.find('[data-testid="vanity-accepts"]').exists()).toBe(true)
      await w.get('[data-testid="vanity-slug"]').setValue('hawks')
      apiMock.mockResolvedValue({ slug: 'hawks', accepts_signups: false, path: '/join/hawks' })
      await w.get('form').trigger('submit')
      await flushPromises()
      expect(apiMock).toHaveBeenLastCalledWith('/api/admin/vanity-link', { method: 'PUT', json: { slug: 'hawks', accepts_signups: false }, decode: expect.any(Function) })
    })
  })

  it('keeps the section with an error and a retry when the address cannot be loaded', async () => {
    useAuthStore().user = userFixture({ role: 'admin' })
    apiMock.mockRejectedValue(new Error('down'))
    const w = mount(AdminVanityLink)
    await flushPromises()
    expect(w.find('[data-testid="vanity-link"]').exists()).toBe(true)
    expect(w.find('[data-testid="vanity-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="vanity-slug"]').exists()).toBe(false)
    apiMock.mockReset().mockResolvedValue({ slug: 'clan', accepts_signups: false, path: '/join/clan' })
    await w.get('[data-testid="state-retry"]').trigger('click')
    await flushPromises()
    expect(w.find<HTMLInputElement>('[data-testid="vanity-slug"]').element.value).toBe('clan')
  })
})
