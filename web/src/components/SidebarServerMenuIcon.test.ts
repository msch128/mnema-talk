import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import SidebarServerMenu from './SidebarServerMenu.vue'

let wrapper: VueWrapper | null = null

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('SidebarServerMenu community icon', () => {
  it('shows the letter mark while the Owner set no icon', () => {
    wrapper = mount(SidebarServerMenu)
    expect(wrapper.find('[data-testid="server-icon"]').exists()).toBe(false)
  })

  it('shows the community icon in the header, and follows a change', async () => {
    const features = useServerFeaturesStore()
    features.setCommunityIcon('abc123')
    wrapper = mount(SidebarServerMenu)
    const img = wrapper.get('[data-testid="server-icon"]')
    expect(img.attributes('src')).toBe('/api/community/icon?size=128&v=abc123')
    expect(img.attributes('alt')).toBe('')

    features.setCommunityIcon('def456')
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="server-icon"]').attributes('src')).toBe('/api/community/icon?size=128&v=def456')

    features.setCommunityIcon('')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="server-icon"]').exists()).toBe(false)
  })
})
