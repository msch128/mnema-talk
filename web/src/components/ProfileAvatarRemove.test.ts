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
let chat: ReturnType<typeof useChatStore>
let toasts: ReturnType<typeof useToastStore>

beforeEach(() => {
  setLocale('en')
  mockApi.mockReset()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture(me)
  chat = useChatStore()
  toasts = useToastStore()
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

describe('remove avatar', () => {
  it('removes the avatar and shows the initial again', async () => {
    open({ ...me, avatar_url: '/api/media/old' }, ['profile.avatar_remove'])
    mockApi.mockResolvedValue(userFixture(me))
    await wrapper.get('[data-testid="avatar-remove"]').trigger('click')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/avatar', expect.objectContaining({ method: 'DELETE' }))
    expect(chat.selectedUserProfile?.avatar_url).toBeUndefined()
    expect(wrapper.find('[data-testid="avatar-remove"]').exists()).toBe(false)
  })

  it('reports a failed removal and keeps the avatar', async () => {
    open({ ...me, avatar_url: '/api/media/old' }, ['profile.avatar_remove'])
    mockApi.mockRejectedValue(new Error('nope'))
    await wrapper.get('[data-testid="avatar-remove"]').trigger('click')
    await flushPromises()
    expect(toasts.toasts.at(-1)?.text).toBe('nope')
    expect(chat.selectedUserProfile?.avatar_url).toBe('/api/media/old')
  })

  it.each([
    ['without an avatar', me, ['profile.avatar_remove']],
    ['on a server without the feature', { ...me, avatar_url: '/api/media/old' }, ['profile.banner']],
    ['on somebody else\'s profile', { ...other, avatar_url: '/api/media/old' }, ['profile.avatar_remove']]
  ])('offers no removal %s', (_name, profile, features) => {
    open(profile, features)
    expect(wrapper.find('[data-testid="avatar-remove"]').exists()).toBe(false)
  })
})
