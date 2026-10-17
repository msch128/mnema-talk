import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import MemberRolesDialog from './MemberRolesDialog.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { PERMISSION_BITS } from '../lib/permissions'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Role } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(20 + n), name: `Role ${n}`, color: n === 1 ? 0xff0000 : 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, ...overrides
})
const target = { id: fixtureId(7), username: 'max', display_name: 'Max', role: 'user' }

function setup(user = target, owner = false) {
  const auth = useAuthStore()
  auth.user = userFixture({ id: fixtureId(1), role: owner ? 'admin' : 'user' })
  const permissions = usePermissionsStore()
  permissions.info = { owner, permissions: PERMISSION_BITS.manage_roles | PERMISSION_BITS.speak, role_ids: [], top_position: 3, catalog: [], channels: [] }
  permissions.roles = [
    role(4, { name: 'Above' }), role(2, { name: 'Speaker', permissions: PERMISSION_BITS.speak }),
    role(1, { name: 'Hooks', permissions: PERMISSION_BITS.manage_webhooks }), role(0, { name: '@everyone', is_everyone: true })
  ]
  permissions.setMemberRoles([{ id: target.id, role_ids: [fixtureId(22)] }])
  return mount(MemberRolesDialog, { props: { user } })
}
const box = (w: ReturnType<typeof mount>, name: string) => w.find(`[data-role="${name}"] input`)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  apiMock.mockResolvedValue(undefined)
})

describe('MemberRolesDialog', () => {
  it('lists the roles with the member’s ticked and locks what the member may not give', () => {
    const w = setup()
    expect(w.text()).toContain('Roles of Max')
    expect(w.findAll('[data-role]').map(li => li.attributes('data-role'))).toEqual(['Above', 'Speaker', 'Hooks'])
    expect((box(w, 'Speaker').element as HTMLInputElement).checked).toBe(true)
    expect(box(w, 'Above').attributes('disabled')).toBeDefined() // at or above their rank
    expect(box(w, 'Hooks').attributes('disabled')).toBeDefined() // permissions they lack
    expect(box(w, 'Speaker').attributes('disabled')).toBeUndefined()
    expect(w.text()).toContain('Above your rank')
  })

  it('saves the chosen set', async () => {
    const w = setup()
    await box(w, 'Speaker').setValue(false)
    await box(w, 'Speaker').setValue(true)
    await box(w, 'Speaker').setValue(false)
    await w.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/users/${target.id}/roles`, { method: 'PUT', json: { role_ids: [] } })
    expect(useToastStore().toasts.map(t => t.text)).toContain('Roles saved')
    expect(w.emitted('saved')).toHaveLength(1)
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('reports a failure and stays open', async () => {
    const w = setup()
    apiMock.mockRejectedValueOnce(new Error('refused'))
    await w.findAll('button').find(b => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('refused')
    expect(w.emitted('close')).toBeUndefined()
  })

  it('freezes the Owner, yourself and members above your rank', async () => {
    for (const [user, hint] of [
      [{ ...target, role: 'admin' }, 'cannot be given roles'],
      [{ ...target, id: fixtureId(1) }, 'cannot change your own roles']
    ] as const) {
      const w = setup(user)
      expect(w.text()).toContain(hint)
      expect(w.findAll('input').every(i => i.attributes('disabled') !== undefined)).toBe(true)
      expect(w.findAll('button').find(b => b.text() === 'Save')!.attributes('disabled')).toBeDefined()
    }
    const w = setup()
    usePermissionsStore().setMemberRoles([{ id: target.id, role_ids: [fixtureId(24)] }])
    await flushPromises()
    expect(w.findAll('input').length).toBe(3)
  })

  it('lets the Owner give any role, and shows an empty state without roles', () => {
    const w = setup(target, true)
    expect(box(w, 'Above').attributes('disabled')).toBeUndefined()
    expect(box(w, 'Hooks').attributes('disabled')).toBeUndefined()
    usePermissionsStore().roles = []
    return w.vm.$nextTick().then(() => expect(w.text()).toContain('No roles yet.'))
  })

  it('closes on cancel', async () => {
    const w = setup()
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('closes from the dialog frame and copes with permissions not loaded yet', async () => {
    const w = setup()
    usePermissionsStore().info = null
    await w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(1)
    expect(w.text()).toContain('Roles of Max')
  })

  it('works for a member with no roles and no display name', () => {
    const w = setup({ id: fixtureId(15), username: 'kim', display_name: '', role: 'user' })
    expect(w.text()).toContain('Roles of kim')
    expect(w.findAll('input').every(i => !(i.element as HTMLInputElement).checked)).toBe(true)
  })
})
