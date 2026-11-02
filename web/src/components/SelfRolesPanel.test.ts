import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import SelfRolesPanel from './SelfRolesPanel.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Role } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const role = (n: number, extra: Partial<Role> = {}): Role => ({
  id: fixtureId(30 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, self_assignable: true, ...extra
})
let open: Role[]
let mine: string[]

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  open = [role(1, { description: 'Game night pings', color: 0x5fd0b6 }), role(2)]
  mine = []
  const permissions = usePermissionsStore()
  vi.spyOn(permissions, 'load').mockImplementation(async () => {
    permissions.info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: mine, top_position: 0, catalog: [], channels: [] }
  })
  apiMock.mockImplementation(async (path, options) => {
    if (path === '/api/roles/self-assignable') return open
    if (path.startsWith('/api/me/roles/')) {
      const id = path.split('/').pop() as string
      mine = options?.method === 'PUT' ? [...mine, id] : mine.filter(r => r !== id)
      return undefined
    }
    return undefined
  })
})

const mountPanel = async () => {
  const w = mount(SelfRolesPanel)
  await flushPromises()
  return w
}
const toggle = (w: Awaited<ReturnType<typeof mountPanel>>, r: Role) => w.get(`[data-testid="self-role-${r.id}"]`)

describe('SelfRolesPanel', () => {
  it('lists the open roles with their description', async () => {
    const w = await mountPanel()
    expect(w.text()).toContain('Role 1')
    expect(w.text()).toContain('Game night pings')
    expect(w.text()).toContain('No description')
    expect(toggle(w, open[0]!).attributes('aria-checked')).toBe('false')
  })

  it('takes and drops a role at once and says Saved', async () => {
    const w = await mountPanel()
    expect(w.get('[data-testid="saved-indicator"]').text()).toBe('')
    await toggle(w, open[0]!).trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/me/roles/${open[0]!.id}`, { method: 'PUT' })
    expect(toggle(w, open[0]!).attributes('aria-checked')).toBe('true')
    expect(w.get('[data-testid="saved-indicator"]').text()).toBe('Saved')
    await toggle(w, open[0]!).trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/me/roles/${open[0]!.id}`, { method: 'DELETE' })
    expect(toggle(w, open[0]!).attributes('aria-checked')).toBe('false')
  })

  it('reports a refused change with a toast and shows what the server holds', async () => {
    const w = await mountPanel()
    apiMock.mockImplementation(async (path, options) => {
      if (path === '/api/roles/self-assignable') return []
      if (options?.method === 'PUT') throw new Error('members cannot take or drop this role themselves')
      return undefined
    })
    await toggle(w, open[0]!).trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('members cannot take or drop this role themselves')
    expect(w.get('[data-testid="saved-indicator"]').text()).toBe('')
    expect(w.find('[data-testid="self-roles-empty"]').exists()).toBe(true)
  })

  it('says so when the list cannot be refreshed after a change, and keeps what is shown', async () => {
    const w = await mountPanel()
    apiMock.mockImplementation(async (path, options) => {
      if (path === '/api/roles/self-assignable') throw new Error('offline')
      return options?.method ? undefined : undefined
    })
    await toggle(w, open[0]!).trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Could not load the roles.')
    expect(w.text()).toContain('Role 1')
  })

  it('offers a retry when the list cannot be loaded', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const w = mount(SelfRolesPanel)
    await flushPromises()
    expect(w.text()).toContain('Could not load the roles.')
    await w.get('[data-testid="self-roles-retry"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Role 1')
  })
})
