import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import BotRolesPanel from './BotRolesPanel.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { PERMISSION_BITS } from '../lib/permissions'
import { fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Role } from '../types/domain'
import type { BotBot } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(20 + n), name: `Role ${n}`, color: 0, position: n, permissions: PERMISSION_BITS.send_messages, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const bot = (roleIds: string[] = []): BotBot => ({ id: fixtureId(5), user_id: fixtureId(6), name: 'Echo', allowed_intents: 0, disabled: false, role_ids: roleIds, created_at: '2026-01-02T03:04:05Z' })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  usePermissionsStore().roles = [role(1, { color: 0xff0000 }), role(2, { name: 'Boss', permissions: PERMISSION_BITS.administrator }), role(3, { name: 'Mod', permissions: PERMISSION_BITS.manage_members }), role(0, { name: '@everyone', is_everyone: true })]
})

describe('BotRolesPanel', () => {
  it('lists roles without @everyone, explains the no-access start and flags powerful roles', () => {
    const w = mount(BotRolesPanel, { props: { bot: bot() } })
    expect(w.findAll('[data-role]').map(r => r.attributes('data-role'))).toEqual(['Role 1', 'Boss', 'Mod'])
    expect(w.text()).toContain('does not get @everyone')
    expect(w.findAll('[data-testid="bot-role-power"]')).toHaveLength(2)
    expect(w.get('[data-testid="bot-roles-save"]').attributes('disabled')).toBeDefined()
  })

  it('shows the strong warning once a powerful role is selected', async () => {
    const w = mount(BotRolesPanel, { props: { bot: bot() } })
    expect(w.find('[data-testid="bot-power-selected"]').exists()).toBe(false)
    await w.get('[data-role="Boss"] input').setValue(true)
    expect(w.find('[data-testid="bot-power-selected"]').exists()).toBe(true)
    await w.get('[data-role="Boss"] input').setValue(false)
    expect(w.find('[data-testid="bot-power-selected"]').exists()).toBe(false)
  })

  it('saves the chosen roles and emits the updated bot', async () => {
    const w = mount(BotRolesPanel, { props: { bot: bot() } })
    const updated = bot([fixtureId(21)])
    apiMock.mockResolvedValueOnce(updated)
    await w.get('[data-role="Role 1"] input').setValue(true)
    await w.get('[data-testid="bot-roles-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/bots/${fixtureId(5)}/roles`, expect.objectContaining({ method: 'PUT', json: { role_ids: [fixtureId(21)] } }))
    expect(w.emitted('updated')?.[0]).toEqual([updated])
    expect(useToastStore().toasts.map(t => t.text)).toContain('Roles saved.')
  })

  it('shows the server message when the roles are refused', async () => {
    const w = mount(BotRolesPanel, { props: { bot: bot() } })
    apiMock.mockRejectedValueOnce(new Error('only the Owner may assign this role'))
    await w.get('[data-role="Boss"] input').setValue(true)
    await w.get('[data-testid="bot-roles-save"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('only the Owner may assign this role')
    expect(w.emitted('updated')).toBeUndefined()
  })

  it('follows role changes of the bot and loads roles when missing', async () => {
    usePermissionsStore().roles = []
    apiMock.mockResolvedValue([])
    const w = mount(BotRolesPanel, { props: { bot: bot() } })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/roles', expect.anything())
    expect(w.text()).toContain('no roles yet')
    usePermissionsStore().roles = [role(1)]
    await w.setProps({ bot: bot([fixtureId(21)]) })
    expect((w.get('[data-role="Role 1"] input').element as HTMLInputElement).checked).toBe(true)
    await w.get('[data-testid="bot-roles-save"]').trigger('click')
    expect(apiMock).toHaveBeenCalledTimes(1)
  })
})
