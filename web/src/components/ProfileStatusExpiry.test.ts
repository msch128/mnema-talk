import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProfileStatus from './ProfileStatus.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { i18nPlugin, setLocale } from '../i18n'
import { dateTime } from '../lib/time'
import { userFixture } from '../test-fixtures.fixture'

let wrapper: VueWrapper
let setStatusText: ReturnType<typeof vi.fn>
const me = userFixture({ status_text: '' })

function open(features: string[]) {
  useServerFeaturesStore().list = features
  wrapper = mount(ProfileStatus, { global: { plugins: [i18nPlugin] }, props: { profileUser: me, isSelf: true } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 24, 14, 30, 0))
  const chat = useChatStore()
  setStatusText = vi.fn().mockResolvedValue({ ...me, status_text: 'Lunch' })
  chat.setStatusText = setStatusText as unknown as typeof chat.setStatusText
})
afterEach(() => {
  wrapper.unmount()
  vi.useRealTimers()
})

async function startEditing(text: string) {
  await wrapper.get('[data-testid="profile-status-edit"]').trigger('click')
  await wrapper.get('[data-testid="profile-status-input"]').setValue(text)
}

describe('status expiry in the profile', () => {
  it('offers no "clear after" choice on a server without the feature', async () => {
    open([])
    await startEditing('Lunch')
    expect(wrapper.find('[data-testid="status-expiry"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, 'Lunch', null)
  })

  it('sends the absolute local time of "tonight"', async () => {
    open(['status.expiry'])
    await startEditing('Lunch')
    await wrapper.get('[data-testid="status-expiry-choice"]').setValue('tonight')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, 'Lunch', new Date(2026, 9, 24, 23, 59))
  })

  it('sends one hour from now, and nothing for "never"', async () => {
    open(['status.expiry'])
    await startEditing('Lunch')
    await wrapper.get('[data-testid="status-expiry-choice"]').setValue('hour')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenLastCalledWith(me.id, 'Lunch', new Date(2026, 9, 24, 15, 30))
  })

  it('refuses a custom time in the past and sends a future one', async () => {
    open(['status.expiry'])
    await startEditing('Lunch')
    await wrapper.get('[data-testid="status-expiry-choice"]').setValue('custom')
    await wrapper.get('[data-testid="status-expiry-custom"]').setValue('2026-10-24T09:00')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toContain('future')
    await wrapper.get('[data-testid="status-expiry-custom"]').setValue('2026-10-26T09:00')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, 'Lunch', new Date(2026, 9, 26, 9, 0))
  })

  it('has no expiry choice while the status text is empty', async () => {
    open(['status.expiry'])
    await startEditing('   ')
    expect(wrapper.find('[data-testid="status-expiry"]').exists()).toBe(false)
  })

  it('writes the clear time in the app language', () => {
    setLocale('de')
    try {
      useServerFeaturesStore().list = ['status.expiry']
      const iso = '2026-10-24T22:00:00Z'
      wrapper = mount(ProfileStatus, {
        global: { plugins: [i18nPlugin] },
        props: { profileUser: { ...me, status_text: 'Lunch', status_expires_at: iso }, isSelf: true }
      })
      const text = wrapper.get('[data-testid="profile-status-until"]').text()
      expect(text).toContain(dateTime(iso, 'de'))
      expect(text).not.toContain(dateTime(iso, 'en'))
    } finally {
      setLocale('en')
    }
  })

  it('shows when a set status clears itself', () => {
    useServerFeaturesStore().list = ['status.expiry']
    wrapper = mount(ProfileStatus, {
      global: { plugins: [i18nPlugin] },
      props: { profileUser: { ...me, status_text: 'Lunch', status_expires_at: '2026-10-24T22:00:00Z' }, isSelf: true }
    })
    expect(wrapper.get('[data-testid="profile-status-until"]').text()).toContain('Clears')
  })
})
