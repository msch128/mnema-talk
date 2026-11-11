import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarServerMenu from '../../components/SidebarServerMenu.vue'
import { useAuthStore } from '../../stores/auth'
import { useChatStore } from '../../stores/chat'
import { usePermissionsStore } from '../../stores/permissions'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { i18nPlugin, setLocale } from '../../i18n'
import { tooltip } from '../../directives/tooltip'
import { fixtureId, userFixture } from '../../test-fixtures.fixture'
import { PERMISSION_BITS } from '../../shared/lib/permissions'

const http = vi.hoisted(() => ({ urls: [] as string[], open: 0, listed: 0, more: false }))
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: vi.fn((url: string) => {
    http.urls.push(url)
    if (url.startsWith('/api/admin/reports?')) return Promise.resolve({ has_more: http.more, reports: Array.from({ length: http.listed }, (_, i) => ({ id: i })) })
    return Promise.resolve(url === '/api/admin/reports/open-count' ? { open: http.open } : [])
  })
}))
vi.mock('../../talk/composables/useWebRTC', () => ({
  useWebRTC: () => ({ leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn() })
}))

const global = { plugins: [i18nPlugin], directives: { tooltip } }
const info = (permissions: number) => ({ owner: false, permissions, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [] })

let w: VueWrapper
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.urls = []
  http.open = 0
  http.listed = 0
  http.more = false
  useAuthStore().user = userFixture({ id: fixtureId(1), role: 'user' })
  useServerFeaturesStore().list = ['reports', 'reports.open_count']
})
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

const mountMenu = () => {
  w = mount(SidebarServerMenu, { global, attachTo: document.body })
  return w
}
const reportsOpenEvent = (open: number) => useChatStore().handleWSEvent({ type: 'reports_open', payload: { open } })

describe('open reports badge', () => {
  it('shows the count on the community header and the Admin console entry of a moderator, with a screen reader sentence', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    http.open = 3
    mountMenu()
    await flushPromises()
    expect(w.findAll('[data-testid="reports-badge"]')).toHaveLength(1) // the header; the menu is closed
    await w.get('[aria-controls="server-menu"]').trigger('click')
    const badges = w.findAll('[data-testid="reports-badge"]')
    expect(badges).toHaveLength(2)
    expect(badges[1]?.text()).toContain('3')
    expect(badges[1]?.get('.sr-only').text()).toBe('3 open reports')
  })

  it('counts the open reports of a server without reports.open_count from the report list (its open-count path reads as a report id)', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    useServerFeaturesStore().list = ['reports']
    http.listed = 3
    mountMenu()
    await flushPromises()
    expect(http.urls).toContain('/api/admin/reports?status=open&limit=100')
    expect(http.urls).not.toContain('/api/admin/reports/open-count')
    expect(w.get('[data-testid="reports-badge"] [aria-hidden="true"]').text()).toBe('3')
  })

  it('shows "n+" when the list of open reports has another page', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    useServerFeaturesStore().list = ['reports']
    http.listed = 3
    http.more = true
    mountMenu()
    await flushPromises()
    expect(w.get('[data-testid="reports-badge"] [aria-hidden="true"]').text()).toBe('3+')
  })

  it('says "1 open report" for a single one', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    http.open = 1
    mountMenu()
    await flushPromises()
    expect(w.get('[data-testid="reports-badge"] .sr-only').text()).toBe('1 open report')
  })

  it('shows nothing to a member without Manage reports, and never asks the server', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_members)
    http.open = 5
    mountMenu()
    await flushPromises()
    reportsOpenEvent(5)
    await flushPromises()
    expect(w.find('[data-testid="reports-badge"]').exists()).toBe(false)
    expect(http.urls).not.toContain('/api/admin/reports/open-count')
  })

  it('follows the reports_open event live and disappears at zero', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    mountMenu()
    await flushPromises()
    expect(w.find('[data-testid="reports-badge"]').exists()).toBe(false)
    reportsOpenEvent(2)
    await flushPromises()
    expect(w.get('[data-testid="reports-badge"]').text()).toContain('2')
    reportsOpenEvent(0)
    await flushPromises()
    expect(w.find('[data-testid="reports-badge"]').exists()).toBe(false)
  })

  it('hides when the role is revoked and comes back with the permission', async () => {
    const permissions = usePermissionsStore()
    permissions.info = info(PERMISSION_BITS.manage_reports)
    http.open = 4
    mountMenu()
    await flushPromises()
    expect(w.find('[data-testid="reports-badge"]').exists()).toBe(true)
    permissions.info = info(0)
    await flushPromises()
    expect(w.find('[data-testid="reports-badge"]').exists()).toBe(false)
    permissions.info = info(PERMISSION_BITS.manage_reports)
    await flushPromises()
    expect(w.get('[data-testid="reports-badge"]').text()).toContain('4')
  })

  it('shortens a large count to 99+ while the screen reader hears the whole number', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    mountMenu()
    await flushPromises()
    reportsOpenEvent(150)
    await flushPromises()
    expect(w.get('[data-testid="reports-badge"] [aria-hidden="true"]').text()).toBe('99+')
    expect(w.get('[data-testid="reports-badge"] .sr-only').text()).toBe('150 open reports')
  })

  it('stays hidden when the community switched reports off', async () => {
    usePermissionsStore().info = info(PERMISSION_BITS.manage_reports)
    useServerFeaturesStore().list = []
    http.open = 4
    mountMenu()
    await flushPromises()
    expect(w.find('[data-testid="reports-badge"]').exists()).toBe(false)
  })
})
