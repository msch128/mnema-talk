import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AccountPrivacy from './AccountPrivacy.vue'
import { setLocale } from '../i18n'
import { useServerFeaturesStore } from '../stores/serverFeatures'

const stubs = { AccountDmPolicy: true, AccountBlockedList: true }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('privacy section', () => {
  it('shows the direct-message policy and the blocked list where the server has them', () => {
    useServerFeaturesStore().list = ['dm', 'blocks']
    const w = mount(AccountPrivacy, { global: { stubs } })
    expect(w.findComponent({ name: 'AccountDmPolicy' }).exists()).toBe(true)
    expect(w.findComponent({ name: 'AccountBlockedList' }).exists()).toBe(true)
    expect(w.find('[data-testid="privacy-empty"]').exists()).toBe(false)
  })

  it('shows only what exists', () => {
    useServerFeaturesStore().list = ['blocks']
    const w = mount(AccountPrivacy, { global: { stubs } })
    expect(w.findComponent({ name: 'AccountDmPolicy' }).exists()).toBe(false)
    expect(w.findComponent({ name: 'AccountBlockedList' }).exists()).toBe(true)
  })

  it('says so when there is nothing to set instead of an empty page', () => {
    useServerFeaturesStore().list = []
    const w = mount(AccountPrivacy, { global: { stubs } })
    expect(w.get('[data-testid="privacy-empty"]').text()).toBe('There is nothing to set here on this server.')
  })
})
