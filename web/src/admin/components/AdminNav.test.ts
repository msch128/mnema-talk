import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminNav from './AdminNav.vue'
import { i18nPlugin, setLocale } from '../../i18n'
import { useReportsOpenStore } from '../../stores/reportsOpen'
import { usePermissionsStore } from '../../stores/permissions'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { PERMISSION_BITS } from '../../shared/lib/permissions'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  usePermissionsStore().info = { owner: false, permissions: PERMISSION_BITS.manage_reports, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] }
  useServerFeaturesStore().list = ['reports', 'reports.open_count']
})

const nav = () => mount(AdminNav, { props: { tabs: ['users', 'reports'], active: 'users' }, global: { plugins: [i18nPlugin] } })

describe('the open-reports count in the admin navigation', () => {
  it('is the shared badge: capped at 99+ for the eye, the whole number for a screen reader, nothing on the label span', () => {
    useReportsOpenStore().set(150)
    const view = nav()
    const badge = view.get('[data-testid="tab-reports"] [data-testid="reports-badge"]')
    expect(badge.get('[aria-hidden="true"]').text()).toBe('99+')
    expect(badge.get('.sr-only').text()).toBe('150 open reports')
    expect(badge.attributes('aria-label')).toBeUndefined()
    expect(view.find('[data-testid="tab-users"] [data-testid="reports-badge"]').exists()).toBe(false)
  })

  it('is absent when nothing is open', () => {
    expect(nav().find('[data-testid="reports-badge"]').exists()).toBe(false)
  })
})
