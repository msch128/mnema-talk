import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, shallowMount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminDashboard from './AdminDashboard.vue'
import { useFirstRunStore } from '../stores/firstRun'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

enableAutoUnmount(afterEach)

let visit: ReturnType<typeof vi.fn>
let reload: ReturnType<typeof vi.fn>

function mountDashboard(initialTab: string) {
  return shallowMount(AdminDashboard, { props: { initialTab } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ role: 'admin' })
  useServerFeaturesStore().list = ['ops.status']
  const store = useFirstRunStore()
  visit = vi.fn().mockResolvedValue(undefined)
  reload = vi.fn().mockResolvedValue(undefined)
  store.visit = visit as never
  store.reload = reload as never
})

describe('the console and the Owner\'s checklist', () => {
  it('reports the channels and status pages as visited, and no other', async () => {
    mountDashboard('channels')
    expect(visit).toHaveBeenCalledWith('channels')
    visit.mockClear()
    mountDashboard('status')
    expect(visit).toHaveBeenCalledWith('status')
    visit.mockClear()
    mountDashboard('invites')
    expect(visit).not.toHaveBeenCalled()
  })

  it('reads the checklist again when the console closes', () => {
    const w = mountDashboard('users')
    expect(reload).not.toHaveBeenCalled()
    w.unmount()
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
