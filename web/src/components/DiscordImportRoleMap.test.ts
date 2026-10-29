import type { ApiOptions } from '../lib/api'
import type { DiscordimportPlan, DiscordimportPlanRole } from '../types/rest'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminImportTab from './AdminImportTab.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
vi.mock('../lib/confirm', () => ({ confirm: vi.fn() }))

const ADMIN_ID = '00000000-0000-4000-8000-000000000a01'
const MODS_ID = '00000000-0000-4000-8000-000000000a02'
const RECORD = { id: '00000000-0000-4000-8000-000000000701', template_code: 'AbCdEf123456', template_name: 'Hub', created_by_name: 'Owner', created_at: '2026-10-20T10:00:00Z', counts: { categories: 0, channels: 0, roles: 1 }, undone_at: null }

function role(id: string, name: string, suggested: string | null): DiscordimportPlanRole {
  return { source_id: id, name, source_name: name, color: 0, hoist: false, mentionable: false, permissions: 8, permission_keys: ['kick_members'], dropped: [], administrator_dropped: false, suggested_role_id: suggested }
}

// What the server answers for a given role_map: mapped roles leave the plan.
function planFor(map: Record<string, string>): DiscordimportPlan {
  const all = [role('13', 'Admin', ADMIN_ID), role('12', 'Moderator', null)]
  const created = all.filter(r => !map[r.source_id])
  return {
    code: 'AbCdEf123456', template_name: 'Hub', source_name: 'Hub', server_name: '', categories: [], channels: [],
    roles: created, mapped_roles: all.filter(r => map[r.source_id]).map(r => ({ source_id: r.source_id, source_name: r.source_name, role_id: map[r.source_id] ?? '', role_name: r.source_id === '13' ? 'Admin' : 'Mods' })),
    mappable_roles: [{ id: ADMIN_ID, name: 'Admin' }, { id: MODS_ID, name: 'Mods' }],
    everyone: { permission_keys: [], dropped: [], applied: false }, skipped: [], dropped_bits: [], administrator_dropped: false,
    counts: { categories: 0, channels: 0, roles: created.length }, hash: `h-${Object.keys(map).sort().join('+')}`
  }
}

const bodies = (suffix: string) => apiMock.mock.calls.filter(([p]) => p.endsWith(suffix)).map(([, o]) => o?.json as Record<string, unknown>)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  apiMock.mockImplementation(async (path, options) => {
    const json = (options?.json ?? {}) as { role_map?: Record<string, string> }
    if (path.endsWith('/preview')) return planFor(json.role_map ?? {})
    if (path.endsWith('/apply')) return RECORD
    if (path.endsWith('/imports')) return []
    throw new Error(`unexpected ${path}`)
  })
})

async function previewed() {
  const w = mount(AdminImportTab)
  await flushPromises()
  await w.get('[data-testid="import-link"]').setValue('AbCdEf123456')
  await w.get('form').trigger('submit')
  await flushPromises()
  return w
}

const selects = (w: Awaited<ReturnType<typeof previewed>>) => w.findAll('[data-testid="import-role-map"]')

describe('Discord import role mapping', () => {
  it('preselects the existing role with the same name and plans again with it', async () => {
    const w = await previewed()
    expect(bodies('/preview')).toEqual([{ template: 'AbCdEf123456' }, { template: 'AbCdEf123456', role_map: { 13: ADMIN_ID } }])
    const [admin, moderator] = selects(w)
    expect((admin?.element as HTMLSelectElement).value).toBe(ADMIN_ID)
    expect((moderator?.element as HTMLSelectElement).value).toBe('')
    expect(w.get('[data-testid="import-role-suggestion"]').text()).toContain('Connected to Admin')
    expect(w.get('[data-testid="import-role-mapped"]').text()).toContain('Not created')
    expect(w.text()).toContain('1 role')
  })

  it('offers to create each role as new by default and lists the existing roles', async () => {
    const w = await previewed()
    const options = selects(w)[1]!.findAll('option').map(o => o.text())
    expect(options).toEqual(['Create as a new role', 'Admin', 'Mods'])
  })

  it('disables a role that another Discord role already joins', async () => {
    const w = await previewed()
    const moderatorOptions = selects(w)[1]!.findAll('option')
    expect(moderatorOptions.find(o => o.text() === 'Admin')!.attributes('disabled')).toBeDefined()
    expect(moderatorOptions.find(o => o.text() === 'Mods')!.attributes('disabled')).toBeUndefined()
  })

  it('plans once more on review when a choice changed, and the import carries that mapping', async () => {
    const w = await previewed()
    await selects(w)[1]!.setValue(MODS_ID)
    await selects(w)[0]!.setValue('')
    expect(bodies('/preview')).toHaveLength(2) // changing a choice alone asks the server nothing
    await w.get('[data-testid="import-review"]').trigger('click')
    await flushPromises()
    expect(bodies('/preview')[2]).toEqual({ template: 'AbCdEf123456', role_map: { 12: MODS_ID } })
    expect(w.find('[data-testid="import-step-1"]').exists()).toBe(true)

    await w.get('[data-testid="import-next"]').trigger('click')
    expect(w.get('[data-testid="import-step-2-mapped"]').text()).toContain('Moderator is not created')
    await w.get('[data-testid="import-next"]').trigger('click')
    await w.get('[data-testid="import-confirm-text"]').setValue('IMPORT')
    await w.get('[data-testid="import-run"]').trigger('click')
    await flushPromises()
    expect(bodies('/apply')[0]).toMatchObject({ plan_hash: 'h-12', role_map: { 12: MODS_ID } })
  })

  it('does not plan again on review when nothing changed, and sends no role_map when nothing is mapped', async () => {
    const w = await previewed()
    await w.get('[data-testid="import-review"]').trigger('click')
    await flushPromises()
    expect(bodies('/preview')).toHaveLength(2)

    await selects(w)[0]!.setValue('')
    await w.get('[data-testid="import-review"]').trigger('click')
    await flushPromises()
    expect(bodies('/preview')[2]).toEqual({ template: 'AbCdEf123456' })
  })

  it('preselects an existing role for one Discord role only when two share a name', async () => {
    apiMock.mockImplementation(async (path, options) => {
      const json = (options?.json ?? {}) as { role_map?: Record<string, string> }
      if (path.endsWith('/preview')) {
        const p = planFor(json.role_map ?? {})
        const all = [role('13', 'Mod', ADMIN_ID), role('14', 'mod', ADMIN_ID)]
        return { ...p, roles: all.filter(r => !json.role_map?.[r.source_id]) }
      }
      if (path.endsWith('/imports')) return []
      throw new Error(`unexpected ${path}`)
    })
    await previewed()
    expect(bodies('/preview')[1]).toEqual({ template: 'AbCdEf123456', role_map: { 13: ADMIN_ID } })
  })
})
