import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminVanityLink from './AdminVanityLink.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

async function mountLink(initial = { slug: '', accepts_signups: false, path: '' }) {
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
    expect(useToastStore().toasts.some(x => x.type === 'error')).toBe(true)
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
})
