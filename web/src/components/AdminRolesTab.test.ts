import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminRolesTab from './AdminRolesTab.vue'
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
const confirmMock = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (...args: unknown[]) => confirmMock(...args) }))

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(20 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const everyone = role(0, { id: fixtureId(19), name: '@everyone', is_everyone: true, permissions: PERMISSION_BITS.view_channel })
let roles: Role[]
const toastTexts = () => useToastStore().toasts.map(t => t.text)

function setup(owner = true) {
  const auth = useAuthStore()
  auth.user = userFixture({ role: owner ? 'admin' : 'user' })
  const permissions = usePermissionsStore()
  permissions.info = { owner, permissions: owner ? 65535 : PERMISSION_BITS.manage_roles | PERMISSION_BITS.speak, max_upload_mb: 50, role_ids: [], top_position: 3, catalog: [], channels: [] }
  apiMock.mockImplementation(async (path, options) => {
    if (path === '/api/roles') return roles
    if (path === '/api/admin/roles' && options?.method === 'POST') return role(9, { name: 'New role', position: 1 })
    if (path.startsWith('/api/admin/roles/') && options?.method === 'PATCH') return role(1)
    if (path.endsWith('/view-as')) return { role_id: path.split('/')[4], role_name: 'Middle', administrator: false, categories: [], uncategorized: [{ id: fixtureId(90), number: 1, name: 'welcome', type: 'text', can_join: false, permissions: ['view_channel'] }] }
    return undefined
  })
  return mount(AdminRolesTab, { attachTo: document.body })
}
const row = (w: ReturnType<typeof mount>, name: string) => w.find(`[data-role="${name}"]`)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  document.body.replaceChildren()
  roles = [role(4, { name: 'Top' }), role(2, { name: 'Middle', color: 0xff0000, hoist: true, permissions: PERMISSION_BITS.speak }), role(1, { name: 'Bottom' }), everyone]
})

describe('AdminRolesTab', () => {
  it('lists roles by rank with @everyone last and opens @everyone first', async () => {
    const w = setup()
    await flushPromises()
    expect(w.findAll('[data-role]').map(li => li.attributes('data-role'))).toEqual(['Top', 'Middle', 'Bottom'])
    expect(w.text()).toContain('@everyone')
    expect(w.text()).toContain('What every member has.')
    // only the permissions of @everyone are editable
    expect(w.find('input[data-max-chars="32"]').exists()).toBe(false)
    expect(w.findAll('[role="switch"]')).toHaveLength(26)
  })

  it('shows the empty state', async () => {
    roles = [everyone]
    const w = setup()
    await flushPromises()
    expect(w.text()).toContain('No roles yet.')
  })

  it('edits and saves a role', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Middle').find('button[aria-pressed]').trigger('click')
    const name = w.find<HTMLInputElement>('input[data-max-chars="32"]')
    expect(name.element.value).toBe('Middle')
    expect(w.find<HTMLInputElement>('input[placeholder="#rrggbb"]').element.value).toBe('#ff0000')
    const save = () => w.get('[data-testid="unsaved-save"]')
    expect(save().attributes('disabled')).toBeDefined()
    await name.setValue('  Moderators ')
    await w.find('[data-permission="send_messages"] [role="switch"]').trigger('click')
    await w.find('[aria-label="#e8c25a"]').trigger('click')
    await w.find('input[type="checkbox"]').setValue(false)
    expect(save().attributes('disabled')).toBeUndefined()
    await save().trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/roles/${roles[1]!.id}`, expect.objectContaining({
      method: 'PATCH',
      json: { name: 'Moderators', color: 0xe8c25a, hoist: false, permissions: PERMISSION_BITS.speak | PERMISSION_BITS.send_messages }
    }))
    expect(toastTexts()).toContain('Role saved')
  })

  it('lets only the Owner set a role upload limit and sends null to inherit', async () => {
    roles = [role(2, { name: 'Middle', max_upload_mb: 20 }), everyone]
    const w = setup()
    await flushPromises()
    await row(w, 'Middle').find('button[aria-pressed]').trigger('click')
    const field = w.find<HTMLInputElement>('input[aria-label="Max upload size"]')
    expect(field.element.value).toBe('20')
    const save = () => w.get('[data-testid="unsaved-save"]')
    await field.setValue('1.5')
    await save().trigger('click')
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringContaining('/api/admin/roles/'), expect.anything())
    await field.setValue('')
    await save().trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/roles/${roles[0]!.id}`, expect.objectContaining({ json: expect.objectContaining({ max_upload_mb: null }) }))

    const manager = setup(false)
    await flushPromises()
    await row(manager, 'Middle').find('button[aria-pressed]').trigger('click')
    expect(manager.find('input[aria-label="Max upload size"]').exists()).toBe(false)
  })

  it('saves @everyone with only its permissions', async () => {
    const w = setup()
    await flushPromises()
    await w.find('[data-permission="speak"] [role="switch"]').trigger('click')
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/roles/${everyone.id}`, expect.objectContaining({ json: { permissions: PERMISSION_BITS.view_channel | PERMISSION_BITS.speak } }))
  })

  it('takes a typed colour, clears it, and rejects an empty name', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Bottom').find('button[aria-pressed]').trigger('click')
    const hex = w.find<HTMLInputElement>('input[placeholder="#rrggbb"]')
    await hex.setValue('#00ff00')
    await hex.setValue('nonsense')
    await w.find('input[data-max-chars="32"]').setValue('Bottom')
    await w.find('[aria-label="No colour"]').trigger('click')
    expect(hex.element.value).toBe('')
    await hex.setValue('#00ff00')
    await w.find('input[data-max-chars="32"]').setValue('   ')
    await w.find('[data-permission="speak"] [role="switch"]').trigger('click')
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="role-name-invalid"]').text()).toBe('Give the role a name.')
    expect(w.get('[data-testid="unsaved-save"]').attributes('disabled')).toBeDefined()
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringContaining('/api/admin/roles/'), expect.anything())
  })

  it('reports a failed save', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Bottom').find('button[aria-pressed]').trigger('click')
    await w.find('input[data-max-chars="32"]').setValue('Other')
    apiMock.mockRejectedValueOnce(new Error('denied'))
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="unsaved-error"]').text()).toBe('denied')
  })

  it('creates a role and selects it', async () => {
    const w = setup()
    await flushPromises()
    roles = [role(9, { name: 'New role', position: 5 }), ...roles]
    await w.findAll('button').find(b => b.text() === 'New role')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/roles', expect.objectContaining({ method: 'POST', json: { name: 'New role' } }))
    expect(toastTexts()).toContain('Role created')
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('New role')
    apiMock.mockRejectedValueOnce(new Error('no'))
    await w.findAll('button').find(b => b.text() === 'New role')!.trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('no')
  })

  it('deletes a role after confirmation', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Bottom').find('button[aria-pressed]').trigger('click')
    const del = () => w.findAll('button').find(b => b.text() === 'Delete role')!
    confirmMock.mockResolvedValueOnce(false)
    await del().trigger('click')
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringContaining('/api/admin/roles/'), { method: 'DELETE' })
    confirmMock.mockResolvedValueOnce(true)
    roles = roles.filter(r => r.name !== 'Bottom')
    await del().trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/roles/${fixtureId(21)}`, { method: 'DELETE' })
    expect(toastTexts()).toContain('Role deleted')
    expect(w.find('[aria-label="Selected role"]').exists()).toBe(false)
    // a failed delete is reported
    await row(w, 'Middle').find('button[aria-pressed]').trigger('click')
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('cannot'))
    await del().trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('cannot')
  })

  it('reorders with the arrows and by dragging', async () => {
    const w = setup()
    await flushPromises()
    await w.find('[aria-label="Move Middle up"]').trigger('click')
    expect(apiMock).toHaveBeenCalledWith('/api/admin/roles/order', { method: 'PUT', json: { role_ids: [roles[1]!.id, roles[0]!.id, roles[2]!.id] } })
    apiMock.mockClear()
    // drag Bottom onto Middle
    await row(w, 'Bottom').trigger('dragstart')
    await row(w, 'Middle').trigger('drop')
    expect(apiMock).toHaveBeenCalledWith('/api/admin/roles/order', { method: 'PUT', json: { role_ids: [roles[0]!.id, roles[2]!.id, roles[1]!.id] } })
    apiMock.mockClear()
    // dropping on itself or without a drag does nothing
    await row(w, 'Middle').trigger('dragstart')
    await row(w, 'Middle').trigger('drop')
    await row(w, 'Middle').trigger('drop')
    expect(apiMock).not.toHaveBeenCalledWith('/api/admin/roles/order', expect.anything())
    // the outer arrows are disabled
    expect(w.find('[aria-label="Move Top up"]').attributes('disabled')).toBeDefined()
    expect(w.find('[aria-label="Move Bottom down"]').attributes('disabled')).toBeDefined()
    await w.find('[aria-label="Move Top up"]').trigger('click')
    // arrows down, dragging over a row and picking @everyone again
    apiMock.mockClear()
    await row(w, 'Middle').trigger('dragover')
    await w.find('[aria-label="Move Top down"]').trigger('click')
    expect(apiMock).toHaveBeenCalledWith('/api/admin/roles/order', expect.anything())
    await w.findAll('button').find(b => b.text() === '@everyone')!.trigger('click')
  })

  it('reports a failed reorder', async () => {
    const w = setup()
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('x'))
    await w.find('[aria-label="Move Middle up"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('x')
  })

  it('shows roles above the member’s rank read-only and locks permissions they lack', async () => {
    const w = setup(false)
    await flushPromises()
    // top_position is 3: Top (4) is above, Middle (2) below
    expect(row(w, 'Top').attributes('draggable')).toBe('false')
    await row(w, 'Top').find('button[aria-pressed]').trigger('click')
    expect(w.text()).toContain('not below your highest role')
    expect(w.find('[data-testid="unsaved-save"]').exists()).toBe(false)
    expect(w.find('input[data-max-chars="32"]').attributes('disabled')).toBeDefined()
    // arrows next to a role above the rank do nothing
    expect(w.find('[aria-label="Move Top down"]').attributes('disabled')).toBeDefined()
    await row(w, 'Middle').find('button[aria-pressed]').trigger('click')
    expect(w.text()).toContain('You can only grant permissions you have yourself.')
    const switches = Object.fromEntries(w.findAll('[data-permission]').map(li => [li.attributes('data-permission'), li.find('[role="switch"]').attributes('disabled') !== undefined]))
    expect(switches['speak']).toBe(false) // held
    expect(switches['manage_bots']).toBe(true) // not held
    // dragging a role above the rank is ignored
    await row(w, 'Top').trigger('dragstart')
    await row(w, 'Middle').trigger('drop')
    expect(apiMock).not.toHaveBeenCalledWith('/api/admin/roles/order', expect.anything())
  })

  it('drops the open role when it disappears', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Bottom').find('button[aria-pressed]').trigger('click')
    roles = roles.filter(r => r.name !== 'Bottom')
    await usePermissionsStore().loadRoles()
    await flushPromises()
    expect(w.find('[aria-label="Selected role"]').exists()).toBe(false)
  })

  it('ignores a second save while one is running and moves that cannot happen', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Bottom').find('button[aria-pressed]').trigger('click')
    await w.find('input[data-max-chars="32"]').setValue('Other')
    const save = w.get('[data-testid="unsaved-save"]')
    void save.trigger('click')
    void save.trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.filter(([path, options]) => path.startsWith('/api/admin/roles/') && options?.method === 'PATCH')).toHaveLength(1)
    const vm = w.vm as unknown as { move: (index: number, delta: number) => Promise<void> }
    apiMock.mockClear()
    await vm.move(0, -1)
    await vm.move(7, 1)
    expect(apiMock).not.toHaveBeenCalled()
  })

  it('opens nothing without @everyone', async () => {
    roles = [role(2, { name: 'Only' })]
    const w = setup()
    await flushPromises()
    expect(w.find('[aria-label="Selected role"]').exists()).toBe(false)
    expect(w.findAll('[data-role]')).toHaveLength(1)
  })
})

describe('AdminRolesTab permission list', () => {
  it('groups the switches under headings and leaves out the reserved webhook permission', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Middle').find('button[aria-pressed]').trigger('click')
    const groups = w.findAll('[data-permission-group]')
    expect(groups.map(g => g.attributes('data-permission-group'))).toEqual(['general', 'text', 'voice', 'moderation', 'administration'])
    expect(groups.map(g => g.find('h4').text())).toEqual(['General', 'Text', 'Voice', 'Moderation', 'Administration'])
    const shown = w.findAll('[data-permission]').map(li => li.attributes('data-permission'))
    expect(shown).not.toContain('manage_webhooks')
    expect(new Set(shown)).toEqual(new Set(Object.keys(PERMISSION_BITS).filter(key => key !== 'manage_webhooks')))
    expect(shown).toHaveLength(new Set(shown).size)
  })

  it('opens a read-only preview of the server as the selected role and exits it', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Middle').find('button[aria-pressed]').trigger('click')
    await w.get('[data-testid="role-view-as"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/roles/${roles[1]!.id}/view-as`, expect.anything())
    expect(document.body.querySelector('[data-testid="view-as-sidebar"]')?.textContent).toContain('welcome')
    document.body.querySelector<HTMLElement>('[data-testid="view-as-exit"]')?.click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="view-as"]')).toBeNull()
    w.unmount()
  })
})

describe('AdminRolesTab unsaved edits', () => {
  const pick = async (w: ReturnType<typeof mount>, name: string) => {
    await row(w, name).find('button[aria-pressed]').trigger('click')
    await flushPromises()
  }

  it('marks unsaved edits in the bar, resets them and clears the mark', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    expect(w.get('[data-testid="unsaved-status"]').text()).toBe('')
    await w.find('input[data-max-chars="32"]').setValue('Renamed')
    expect(w.get('[data-testid="unsaved-status"]').text()).toBe('You have unsaved changes')
    await w.get('[data-testid="unsaved-reset"]').trigger('click')
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('Middle')
    expect(w.get('[data-testid="unsaved-status"]').text()).toBe('')
  })

  it('does not count spacing, the typed colour text or nothing-for-@everyone fields as changes', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    const save = () => w.get('[data-testid="unsaved-save"]')
    await w.find('input[data-max-chars="32"]').setValue(' Middle  ')
    await w.get<HTMLInputElement>('input[placeholder="#rrggbb"]').setValue('#FF0000')
    expect(save().attributes('disabled')).toBeDefined()
    await w.get<HTMLInputElement>('input[placeholder="#rrggbb"]').setValue('#00ff00')
    expect(save().attributes('disabled')).toBeUndefined()
    await w.get<HTMLInputElement>('input[placeholder="#rrggbb"]').setValue('#ff0000')
    expect(save().attributes('disabled')).toBeDefined()
  })

  it('stores nothing before Save and shows a failed save inline', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Bottom')
    await w.find('input[data-max-chars="32"]').setValue('Other')
    expect(apiMock.mock.calls.some(([, o]) => o?.method === 'PATCH')).toBe(false)
    apiMock.mockRejectedValueOnce(new Error('denied'))
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="unsaved-error"]').text()).toBe('denied')
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('Other')
  })

  it('asks before another role replaces unsaved edits, and keeps them on cancel', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    await w.find('input[data-max-chars="32"]').setValue('Renamed')
    confirmMock.mockResolvedValueOnce(false)
    await pick(w, 'Bottom')
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('Renamed')
    confirmMock.mockResolvedValueOnce(true)
    await pick(w, 'Bottom')
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('Bottom')
  })

  it('switches without asking when nothing changed', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    await pick(w, 'Bottom')
    expect(confirmMock).not.toHaveBeenCalled()
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('Bottom')
  })

  it('does not save the role when Enter is pressed in the permission search', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    await w.get('input[data-max-chars="32"]').setValue('Renamed')
    apiMock.mockClear()
    await w.get('[data-testid="role-permission-search"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(apiMock.mock.calls.some(([, o]) => o?.method === 'PATCH')).toBe(false)
  })

  it('restores the colour box on Reset and ignores how a colour is spelled', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    const hex = w.get<HTMLInputElement>('input[placeholder="#rrggbb"]')
    const before = hex.element.value
    await hex.setValue('#12345')
    await w.get('[data-testid="unsaved-reset"]').trigger('click')
    expect(hex.element.value).toBe(before)
    expect(w.find('[data-testid="role-color-invalid"]').exists()).toBe(false)
    expect(w.get('[data-testid="unsaved-save"]').attributes('disabled')).toBeDefined()
  })

  it('treats an emptied colour box as "no colour" and saves that colour', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    const hex = w.get<HTMLInputElement>('input[placeholder="#rrggbb"]')
    await hex.setValue('#ff0000')
    apiMock.mockClear()
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    await hex.setValue('')
    expect(w.get('[data-testid="unsaved-save"]').attributes('disabled')).toBeUndefined()
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    const patch = apiMock.mock.calls.filter(([, o]) => o?.method === 'PATCH').pop()
    expect((patch?.[1]?.json as { color: number }).color).toBe(0)
  })

  it('narrows the permission list by what is typed', async () => {
    const w = setup()
    await flushPromises()
    const all = w.findAll('[data-permission]').length
    await w.get('[data-testid="role-permission-search"]').setValue('speak')
    const some = w.findAll('[data-permission]').length
    expect(some).toBeGreaterThan(0)
    expect(some).toBeLessThan(all)
    await w.get('[data-testid="role-permission-search"]').setValue('zzzz-nothing')
    expect(w.findAll('[data-permission]')).toHaveLength(0)
    expect(w.get('[data-testid="role-permission-none"]').text()).toBe('No permission matches.')
  })

  it('asks before creating a role would drop unsaved edits', async () => {
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    await w.find('input[data-max-chars="32"]').setValue('Renamed')
    confirmMock.mockResolvedValueOnce(false)
    await w.findAll('button').find(b => b.text() === 'New role')!.trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(apiMock).not.toHaveBeenCalledWith('/api/admin/roles', expect.objectContaining({ method: 'POST' }))
    expect(w.find<HTMLInputElement>('input[data-max-chars="32"]').element.value).toBe('Renamed')
  })

  it('forgets a cancelled drag so a later drop cannot reorder', async () => {
    const w = setup()
    await flushPromises()
    await row(w, 'Bottom').trigger('dragstart')
    await row(w, 'Bottom').trigger('dragend')
    await row(w, 'Middle').trigger('drop')
    expect(apiMock).not.toHaveBeenCalledWith('/api/admin/roles/order', expect.anything())
  })

  it('explains an unusable upload limit or colour instead of leaving Save silently off', async () => {
    roles = [role(2, { name: 'Middle', max_upload_mb: 20 }), everyone]
    const w = setup()
    await flushPromises()
    await pick(w, 'Middle')
    await w.find<HTMLInputElement>('input[aria-label="Max upload size"]').setValue('0')
    expect(w.get('[data-testid="role-upload-invalid"]').text()).toContain('whole number')
    expect(w.find('input[aria-label="Max upload size"]').attributes('aria-invalid')).toBe('true')
    await w.find('input[aria-label="Max upload size"]').setValue('20')
    expect(w.find('[data-testid="role-upload-invalid"]').exists()).toBe(false)
    const hex = w.get<HTMLInputElement>('input[placeholder="#rrggbb"]')
    await hex.setValue('#12345')
    expect(w.find('[data-testid="role-color-invalid"]').exists()).toBe(true)
    await hex.setValue('#123456')
    expect(w.find('[data-testid="role-color-invalid"]').exists()).toBe(false)
  })
})

