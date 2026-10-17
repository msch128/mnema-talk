import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import ChannelPermissionsDialog from './ChannelPermissionsDialog.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
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

const channel = { id: fixtureId(2), name: 'general' }
const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(20 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const everyone = role(0, { id: fixtureId(19), name: '@everyone', is_everyone: true })
const max = userFixture({ id: fixtureId(7), username: 'max', display_name: 'Max' })
const boss = userFixture({ id: fixtureId(8), username: 'boss', display_name: 'Boss' })
const owner = userFixture({ id: fixtureId(9), username: 'herzog', role: 'admin' })
const toastTexts = () => useToastStore().toasts.map(t => t.text)

function setup(props: { kind: 'channel' | 'category' } = { kind: 'channel' }, existing: unknown[] = [], isOwner = false) {
  useAuthStore().user = userFixture({ id: fixtureId(1), role: isOwner ? 'admin' : 'user' })
  const permissions = usePermissionsStore()
  permissions.info = {
    owner: isOwner, permissions: PERMISSION_BITS.manage_channels | PERMISSION_BITS.view_channel | PERMISSION_BITS.speak, max_upload_mb: 50, role_ids: [], top_position: 3, catalog: [],
    channels: [{ channel_id: channel.id, permissions: PERMISSION_BITS.view_channel | PERMISSION_BITS.speak | PERMISSION_BITS.manage_channels }]
  }
  permissions.roles = [role(5, { name: 'Above' }), role(2, { name: 'Staff' }), everyone]
  permissions.setMemberRoles([{ id: max.id }, { id: boss.id, role_ids: [fixtureId(25)] }])
  useChatStore().members = [max, boss, owner]
  apiMock.mockImplementation(async (path, options) => (options?.method === 'PUT' ? undefined : { overwrites: existing }))
  return mount(ChannelPermissionsDialog, { props: { ...props, target: channel } })
}
const select = (w: ReturnType<typeof mount>, label: string) => w.find<HTMLSelectElement>(`select[aria-label="${label}"]`)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('ChannelPermissionsDialog', () => {
  it('loads and lists the entries of a channel', async () => {
    const w = setup({ kind: 'channel' }, [{ type: 'role', id: everyone.id, allow: 0, deny: PERMISSION_BITS.send_messages }, { type: 'member', id: max.id, allow: PERMISSION_BITS.speak, deny: 0 }])
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${channel.id}/overwrites`, { decode: expect.any(Function) })
    expect(w.text()).toContain('Permissions of #general')
    expect(w.findAll('[data-entry]').map(li => li.text())).toEqual(['@everyoneRole', 'MaxMember'])
    expect(w.find('[data-state="deny"][aria-pressed="true"]').exists()).toBe(true)
  })

  it('uses the category routes and wording', async () => {
    const w = setup({ kind: 'category' })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/categories/${channel.id}/overwrites`, { decode: expect.any(Function) })
    expect(w.text()).toContain('Permissions of the category general')
    expect(w.text()).toContain('No special permissions here.')
  })

  it('adds a role and a member, edits them, and saves only non-empty entries', async () => {
    const w = setup()
    await flushPromises()
    expect(select(w, 'Add a role').findAll('option').map(o => o.text())).toEqual(['Add a role', 'Staff', '@everyone'])
    expect(select(w, 'Add a member').findAll('option').map(o => o.text())).toEqual(['Add a member', 'Max'])
    await select(w, 'Add a role').setValue(fixtureId(22))
    await select(w, 'Add a member').setValue(max.id)
    await select(w, 'Add a member').setValue('')
    expect(w.findAll('[data-entry]')).toHaveLength(2)
    await w.find('[data-permission="speak"] [data-state="deny"]').trigger('click')
    await w.find('[data-permission="speak"] [data-state="allow"]').trigger('click')
    await w.find('[data-entry="role:' + fixtureId(22) + '"] button[aria-pressed]').trigger('click')
    await w.find('[data-permission="speak"] [data-state="deny"]').trigger('click')
    await w.findAll('button').find(b => b.text() === 'Save permissions')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/admin/channels/${channel.id}/overwrites`, {
      method: 'PUT',
      json: { overwrites: [{ type: 'role', id: fixtureId(22), allow: 0, deny: PERMISSION_BITS.speak }, { type: 'member', id: max.id, allow: PERMISSION_BITS.speak, deny: 0 }] }
    })
    expect(toastTexts()).toContain('Permissions saved')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('removes an entry and drops empty ones on save', async () => {
    const w = setup({ kind: 'channel' }, [{ type: 'role', id: fixtureId(22), allow: 0, deny: PERMISSION_BITS.speak }, { type: 'member', id: max.id, allow: PERMISSION_BITS.speak, deny: 0 }])
    await flushPromises()
    await w.find('[aria-label="Remove Staff"]').trigger('click')
    await w.find('[aria-label="Remove Max"]').trigger('click')
    expect(w.text()).toContain('No special permissions here.')
    await w.findAll('button').find(b => b.text() === 'Save permissions')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(expect.anything(), { method: 'PUT', json: { overwrites: [] } })
    // an entry that was added but left empty is not sent
    const again = setup()
    await flushPromises()
    await select(again, 'Add a role').setValue(everyone.id)
    await again.findAll('button').find(b => b.text() === 'Save permissions')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(expect.anything(), { method: 'PUT', json: { overwrites: [] } })
  })

  it('makes a channel private with one click, also over an existing entry', async () => {
    const w = setup()
    await flushPromises()
    await w.findAll('button').find(b => b.text() === 'Make private')!.trigger('click')
    expect(w.find(`[data-entry="role:${everyone.id}"]`).exists()).toBe(true)
    expect(w.find('[data-permission="view_channel"] [data-state="deny"][aria-pressed="true"]').exists()).toBe(true)
    await w.find('[data-permission="view_channel"] [data-state="allow"]').trigger('click')
    await w.findAll('button').find(b => b.text() === 'Make private')!.trigger('click')
    expect(w.find('[data-permission="view_channel"] [data-state="deny"][aria-pressed="true"]').exists()).toBe(true)
  })

  it('shows entries of higher roles and members read-only, and locks permissions it lacks', async () => {
    const w = setup({ kind: 'channel' }, [{ type: 'role', id: fixtureId(25), allow: 0, deny: PERMISSION_BITS.speak }, { type: 'member', id: boss.id, allow: 0, deny: PERMISSION_BITS.speak }])
    await flushPromises()
    expect(w.find('[aria-label="Remove Above"]').exists()).toBe(false)
    expect(w.find('[aria-label="Remove Boss"]').exists()).toBe(false)
    expect(w.find('[data-permission="speak"] [data-state="allow"]').attributes('disabled')).toBeDefined()
    // roles above the rank and members above it cannot be added
    expect(select(w, 'Add a role').findAll('option').map(o => o.text())).not.toContain('Above')
    expect(select(w, 'Add a member').findAll('option').map(o => o.text())).toEqual(['Add a member', 'Max'])
    // a permission the editor lacks in the channel is locked, one they hold is not
    await w.find('[data-entry="role:' + everyone.id + '"]').exists()
    const free = setup()
    await flushPromises()
    await select(free, 'Add a role').setValue(everyone.id)
    expect(free.find('[data-permission="manage_messages"] [data-state="deny"]').attributes('disabled')).toBeDefined()
    expect(free.find('[data-permission="speak"] [data-state="deny"]').attributes('disabled')).toBeUndefined()
  })

  it('uses server-wide permissions for a category and lets the Owner do everything', async () => {
    const category = setup({ kind: 'category' })
    await flushPromises()
    await select(category, 'Add a role').setValue(everyone.id)
    expect(category.find('[data-permission="speak"] [data-state="deny"]').attributes('disabled')).toBeUndefined()
    expect(category.find('[data-permission="connect"] [data-state="deny"]').attributes('disabled')).toBeDefined()
    const ownerView = setup({ kind: 'channel' }, [], true)
    await flushPromises()
    await select(ownerView, 'Add a role').setValue(fixtureId(25))
    expect(ownerView.find('[data-permission="manage_messages"] [data-state="deny"]').attributes('disabled')).toBeUndefined()
  })

  it('reports a failed load and a failed save', async () => {
    const failing = setup()
    apiMock.mockReset()
    apiMock.mockRejectedValue(new Error('gone'))
    const w = mount(ChannelPermissionsDialog, { props: { kind: 'channel', target: channel } })
    await flushPromises()
    expect(toastTexts()).toContain('gone')
    expect(w.findAll('button').find(b => b.text() === 'Save permissions')!.attributes('disabled')).toBeDefined()
    const ok = setup()
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('refused'))
    await ok.findAll('button').find(b => b.text() === 'Save permissions')!.trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('refused')
    expect(ok.emitted('close')).toBeUndefined()
    await ok.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(ok.emitted('close')).toHaveLength(1)
    void failing
  })

  it('has no private shortcut without @everyone or the View permission, and ignores edits without a selection', async () => {
    const w = setup()
    await flushPromises()
    usePermissionsStore().roles = [role(2, { name: 'Staff' })]
    await w.vm.$nextTick()
    expect(w.findAll('button').some(b => b.text() === 'Make private')).toBe(false)
    const vm = w.vm as unknown as { update: (field: 'allow' | 'deny', value: number) => void; makePrivate: () => void }
    vm.update('allow', 1)
    vm.makePrivate()
    expect(w.findAll('[data-entry]')).toHaveLength(0)
    usePermissionsStore().info = null
    usePermissionsStore().roles = [everyone]
    await w.vm.$nextTick()
    expect(w.findAll('button').some(b => b.text() === 'Make private')).toBe(true) // defaults hold View
  })

  it('names entries whose role or member is unknown by their id, or by the username', async () => {
    const kay = userFixture({ id: fixtureId(12), username: 'kay', display_name: '' })
    const w = setup({ kind: 'channel' }, [
      { type: 'role', id: fixtureId(88), allow: 0, deny: PERMISSION_BITS.speak },
      { type: 'member', id: fixtureId(89), allow: PERMISSION_BITS.speak, deny: 0 },
      { type: 'member', id: kay.id, allow: PERMISSION_BITS.speak, deny: 0 },
      { type: 'role', id: everyone.id, allow: 0, deny: PERMISSION_BITS.view_channel }
    ])
    useChatStore().members = [max, boss, owner, kay]
    await flushPromises()
    expect(w.findAll('[data-entry]').map(li => li.text().replace(/(Role|Member)$/, ''))).toEqual([fixtureId(88), fixtureId(89), 'kay', '@everyone'])
    expect(select(w, 'Add a member').findAll('option').map(o => o.text())).not.toContain('kay')
    // making it private changes the @everyone entry and leaves the others alone
    await w.findAll('button').find(b => b.text() === 'Make private')!.trigger('click')
    expect(w.findAll('[data-entry]')).toHaveLength(4)
  })

  it('lists members without a display name by their username', async () => {
    const w = setup()
    useChatStore().members = [max, userFixture({ id: fixtureId(13), username: 'lee', display_name: '' })]
    await flushPromises()
    expect(select(w, 'Add a member').findAll('option').map(o => o.text())).toEqual(['Add a member', 'Max', 'lee'])
  })
})
