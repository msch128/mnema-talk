import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminDashboard from './AdminDashboard.vue'
import { PERMISSION_BITS } from '../lib/permissions'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('the sign-in and messages page', () => {
  it('is open to an Administrator who is not the Owner', () => {
    useAuthStore().user = userFixture({ role: 'user' })
    usePermissionsStore().info = { permissions: PERMISSION_BITS.administrator, catalog: [], channels: [], role_ids: [], max_upload_mb: 50, owner: false, top_position: 0 }
    const w = mount(AdminDashboard, { props: { initialTab: 'access' }, global: { stubs: { AdminAccessSettings: { template: '<i data-testid="access-tab" />' } }, directives: { tooltip: () => undefined } } })
    expect(w.find('[data-testid="access-tab"]').exists()).toBe(true)
  })

  it('is closed to a member without the Administrator permission', () => {
    useAuthStore().user = userFixture({ role: 'user' })
    usePermissionsStore().info = { permissions: 0, catalog: [], channels: [], role_ids: [], max_upload_mb: 50, owner: false, top_position: 0 }
    const w = mount(AdminDashboard, { props: { initialTab: 'access' }, global: { stubs: { AdminAccessSettings: { template: '<i data-testid="access-tab" />' } }, directives: { tooltip: () => undefined } } })
    expect(w.find('[data-testid="access-tab"]').exists()).toBe(false)
  })
})
