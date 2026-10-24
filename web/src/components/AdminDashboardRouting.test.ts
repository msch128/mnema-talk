import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminDashboard from './AdminDashboard.vue'
import BaseDialog from './BaseDialog.vue'
import { userFixture } from '../test-fixtures.fixture'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'
import { useCompactLayout } from '../composables/useCompactLayout'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))
// The live operations panel has its own tests and endpoint.
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  useAuthStore().user = userFixture({ role: 'admin', username: 'owner' }) // the Owner may do everything
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})
describe('dashboard route synchronization', () => {
  it('accepts valid prop changes, ignores invalid props, and emits close', async () => {
    apiMock.mockResolvedValue([])
    useAuthStore().user = userFixture({ username: 'admin', display_name: '', role: 'admin' })
    const w = keep(
      mount(AdminDashboard, {
        props: { initialTab: 'invalid' },
        global: {
          stubs: {
            AdminUsersTab: true,
            AdminLayoutTab: true,
            AdminMediaTab: true,
            AdminInvitesTab: true,
            AdminSystemTab: true,
          },
        },
      }),
    )
    expect(w.find('[data-testid="tab-users"]').attributes('aria-pressed')).toBe(
      'true',
    )
    await w.setProps({ initialTab: 'system' })
    expect(
      w.find('[data-testid="tab-system"]').attributes('aria-pressed'),
    ).toBe('true')
    await w.setProps({ initialTab: '' })
    await w.setProps({ initialTab: 'invalid' })
    expect(
      w.find('[data-testid="tab-system"]').attributes('aria-pressed'),
    ).toBe('true')
    window.history.replaceState(null, '', '/admin/media')
    await w.find('[data-testid="tab-media"]').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(1)
  })
  it('renders an empty signed-in name safely', () => {
    apiMock.mockResolvedValue([])
    const w = keep(
      mount(AdminDashboard, { global: { stubs: { AdminUsersTab: true } } }),
    )
    expect(w.text()).toContain('Signed in as')
  })
  it('keeps the console title and shows who is signed in once, in the sidebar', async () => {
    apiMock.mockResolvedValue([])
    const w = keep(
      mount(AdminDashboard, { global: { stubs: { AdminUsersTab: true, AdminSystemTab: true } } }),
    )
    expect(w.get('h2').text()).toBe('Admin console')
    expect(w.get('[data-testid="tab-users"]').attributes('aria-pressed')).toBe('true')
    expect(w.findAll('[data-testid="admin-signed-in"]')).toHaveLength(1)
    expect(w.get('[data-testid="admin-signed-in"]').text()).toContain('Signed in as')
    await w.get('[data-testid="tab-system"]').trigger('click')
    expect(w.get('[data-testid="tab-system"]').attributes('aria-pressed')).toBe('true')
  })

  it('groups the tabs by purpose', () => {
    apiMock.mockResolvedValue([])
    const w = keep(mount(AdminDashboard, { global: { stubs: { AdminUsersTab: true } } }))
    const groups = w.findAll('[data-group]').map(g => g.attributes('data-group'))
    expect(groups).toEqual(['community', 'moderation', 'integrations', 'operations'])
  })
})

describe('dashboard on a phone', () => {
  const { isCompact } = useCompactLayout()
  afterEach(() => { isCompact.value = false })

  it('opens on the list of sections, shows one section with a back arrow and returns to the list', async () => {
    isCompact.value = true
    apiMock.mockResolvedValue([])
    const w = keep(mount(AdminDashboard, { global: { stubs: { AdminUsersTab: true, AdminSystemTab: true } } }))
    expect(w.get('h2').text()).toBe('Admin console')
    expect(w.find('[data-testid="admin-back"]').exists()).toBe(false)
    await w.get('[data-testid="tab-system"]').trigger('click')
    expect(w.find('[data-testid="admin-back"]').exists()).toBe(true)
    expect(w.find('[data-testid="admin-tabs"]').exists()).toBe(false)
    await w.get('[data-testid="admin-back"]').trigger('click')
    expect(w.get('h2').text()).toBe('Admin console')
    expect(w.find('[data-testid="tab-system"]').attributes('aria-current')).toBe('page')
  })

  it('opens the section named by the address straight away', () => {
    isCompact.value = true
    apiMock.mockResolvedValue([])
    const w = keep(mount(AdminDashboard, { props: { initialTab: 'system' }, global: { stubs: { AdminSystemTab: true } } }))
    expect(w.get('h2').text()).toBe('Admin console')
    expect(w.find('[data-testid="admin-back"]').exists()).toBe(true)
  })
})
