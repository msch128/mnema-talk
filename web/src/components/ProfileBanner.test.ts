import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UserProfileModal from './UserProfileModal.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const me = { id: '00000000-0000-4000-8000-0000000003e8', username: 'ada', display_name: 'Ada', role: 'user' as const, bio: '', status_text: '', locale: 'en', created_at: '2026-01-01T00:00:00Z' }
const other = { ...me, id: '00000000-0000-4000-8000-0000000003e9', username: 'bob', display_name: 'Bob' }

let wrapper: VueWrapper
let auth: ReturnType<typeof useAuthStore>
let chat: ReturnType<typeof useChatStore>
let toasts: ReturnType<typeof useToastStore>

beforeEach(() => {
  setLocale('en')
  mockApi.mockReset()
  setActivePinia(createPinia())
  auth = useAuthStore()
  chat = useChatStore()
  toasts = useToastStore()
  auth.user = userFixture(me)
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

function open(profile: Record<string, unknown>, features: string[]) {
  useServerFeaturesStore().list = features
  chat.selectedUserProfile = { ...profile } as typeof chat.selectedUserProfile
  wrapper = mount(UserProfileModal, { attachTo: document.body })
}

async function choose(file: File) {
  const input = wrapper.get<HTMLInputElement>('[data-testid="banner-input"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
  await input.trigger('change')
  await flushPromises()
}

describe('profile banner', () => {
  it('shows the banner as a cover image in the header', () => {
    open({ ...other, banner_url: '/api/media/banner-1' }, ['profile.banner'])
    const img = wrapper.get('[data-testid="profile-banner"]')
    expect(img.element.tagName).toBe('IMG')
    expect(img.attributes('src')).toBe('/api/media/banner-1')
    expect(img.classes()).toContain('object-cover')
    expect(img.attributes('style')).toBeUndefined()
  })

  it('keeps the default pattern without a banner', () => {
    open(other, ['profile.banner'])
    expect(wrapper.find('[data-testid="profile-banner"]').exists()).toBe(false)
  })

  it('offers no banner controls on somebody else\'s profile', () => {
    open(other, ['profile.banner'])
    expect(wrapper.find('[data-testid="profile-banner-controls"]').exists()).toBe(false)
  })

  it('hides the controls on a server without the feature', () => {
    open(me, ['dm'])
    expect(wrapper.find('[data-testid="profile-banner-controls"]').exists()).toBe(false)
  })

  it('offers no removal while there is no banner', () => {
    open(me, ['profile.banner'])
    expect(wrapper.find('[data-testid="banner-change"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="banner-remove"]').exists()).toBe(false)
  })

  it('opens the file picker from "Change banner"', async () => {
    open(me, ['profile.banner'])
    const click = vi.spyOn(wrapper.get<HTMLInputElement>('[data-testid="banner-input"]').element, 'click')
    await wrapper.get('[data-testid="banner-change"]').trigger('click')
    expect(click).toHaveBeenCalledOnce()
  })

  it('uploads the chosen picture and shows it at once', async () => {
    open(me, ['profile.banner'])
    mockApi.mockResolvedValue(userFixture({ ...me, banner_url: '/api/media/new-banner' }))
    await choose(new File(['image'], 'wide.png', { type: 'image/png' }))
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/banner', expect.objectContaining({ method: 'PUT', form: expect.any(FormData) }))
    const form = mockApi.mock.calls[0]?.[1]?.form
    expect(form?.get('banner')).toBeInstanceOf(File)
    expect(chat.selectedUserProfile?.banner_url).toBe('/api/media/new-banner')
    expect(wrapper.get('[data-testid="profile-banner"]').attributes('src')).toBe('/api/media/new-banner')
    expect(wrapper.find('[data-testid="banner-remove"]').exists()).toBe(true)
  })

  it('rejects a picture over 5 MB before sending it', async () => {
    open(me, ['profile.banner'])
    await choose(new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'huge.png', { type: 'image/png' }))
    expect(mockApi).not.toHaveBeenCalled()
    expect(toasts.toasts.at(-1)?.text).toContain('5 MB')
  })

  it('reports a failed upload and allows another try', async () => {
    open(me, ['profile.banner'])
    mockApi.mockRejectedValue(new Error('Banner rejected'))
    await choose(new File(['image'], 'wide.png', { type: 'image/png' }))
    expect(toasts.toasts.at(-1)?.text).toBe('Banner rejected')
    expect(wrapper.get('[data-testid="banner-change"]').attributes('disabled')).toBeUndefined()
  })

  it('removes the banner and falls back to the default header', async () => {
    open({ ...me, banner_url: '/api/media/old' }, ['profile.banner'])
    mockApi.mockResolvedValue(userFixture(me))
    await wrapper.get('[data-testid="banner-remove"]').trigger('click')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/banner', expect.objectContaining({ method: 'DELETE' }))
    expect(chat.selectedUserProfile?.banner_url).toBeUndefined()
    expect(wrapper.find('[data-testid="profile-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="banner-remove"]').exists()).toBe(false)
  })

  it('reports a failed removal and keeps the banner', async () => {
    open({ ...me, banner_url: '/api/media/old' }, ['profile.banner'])
    mockApi.mockRejectedValue(new Error('nope'))
    await wrapper.get('[data-testid="banner-remove"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="profile-banner"]').exists()).toBe(true)
    expect(toasts.toasts.at(-1)?.text).toBe('nope')
  })
})
