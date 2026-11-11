import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarWikiEntry from './SidebarWikiEntry.vue'
import { currentRoute, navigate } from '../shared/lib/router'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { grantWiki, VIEW_WIKI } from '../wiki.fixture'

enableAutoUnmount(afterEach)

beforeEach(() => {
  setActivePinia(createPinia())
  navigate('/', { replace: true })
  useServerFeaturesStore().list = ['wiki']
  grantWiki(VIEW_WIKI)
})

describe('SidebarWikiEntry', () => {
  it('opens the wiki', async () => {
    const w = mount(SidebarWikiEntry)
    await w.get('[data-testid="sidebar-wiki"]').trigger('click')
    expect(currentRoute.value).toMatchObject({ view: 'wiki', wiki: { mode: 'home' } })
  })

  it('is hidden when the server has no wiki', () => {
    useServerFeaturesStore().list = []
    expect(mount(SidebarWikiEntry).find('button').exists()).toBe(false)
  })

  it('is hidden from a member who may not view it', () => {
    grantWiki(0)
    expect(mount(SidebarWikiEntry).find('button').exists()).toBe(false)
  })
})
