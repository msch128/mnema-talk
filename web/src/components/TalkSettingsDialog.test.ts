import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import TalkSettingsDialog from './TalkSettingsDialog.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { PERMISSION_BITS } from '../lib/permissions'
import { channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Channel, Role } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const JOIN = PERMISSION_BITS.view_channel | PERMISSION_BITS.connect
const me = fixtureId(1)
const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(20 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const everyone = role(0, { id: fixtureId(19), name: '@everyone', is_everyone: true })
const staff = role(2, { name: 'Staff' })

function setup(channel: Channel, { manager }: { manager: boolean }, existing: unknown[] = []) {
  useAuthStore().user = userFixture({ id: me })
  useServerFeaturesStore().list = ['talks.lobby']
  const permissions = usePermissionsStore()
  permissions.info = {
    owner: false, permissions: manager ? PERMISSION_BITS.manage_channels : 0, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: []
  }
  permissions.roles = [staff, everyone]
  const chat = useChatStore()
  chat.fetchChannels = vi.fn(async () => {})
  apiMock.mockImplementation(async (path, options) => {
    if (path.endsWith('/overwrites')) return options?.method === 'PUT' ? undefined : { overwrites: existing }
    return channel
  })
  return mount(TalkSettingsDialog, { props: { channel } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('TalkSettingsDialog', () => {
  it('offers a manager the name, the lobby switch and every preset', async () => {
    const w = setup(channelFixture({ type: 'voice', name: 'Games' }), { manager: true })
    await flushPromises()
    expect(w.find('[data-testid="talk-name"]').exists()).toBe(true)
    expect(w.find('[data-testid="talk-lobby"]').exists()).toBe(true)
    for (const id of ['preset-everyone', 'preset-roles', 'preset-limit']) expect(w.find(`[data-testid="${id}"]`).exists()).toBe(true)
  })

  it('offers the leader of a temporary Talk the name and the limit, not access or the lobby switch', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Talk 1', is_temp: true, leader_id: me, user_limit: 4 })
    const w = setup(talk, { manager: false })
    await flushPromises()
    expect(w.find('[data-testid="talk-lobby"]').exists()).toBe(false)
    expect(w.find('[data-testid="preset-everyone"]').exists()).toBe(false)
    expect(w.find('[data-testid="preset-roles"]').exists()).toBe(false)

    await w.find('[data-testid="preset-limit-input"]').setValue(7)
    await w.find('[data-testid="talk-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/talks/${talk.id}`, expect.objectContaining({ method: 'PATCH', json: { user_limit: 7 } }))

    await w.find('[data-testid="talk-name"]').setValue('Raid night')
    await w.find('[data-testid="talk-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/talks/${talk.id}`, expect.objectContaining({ json: { name: 'Raid night' } }))
  })

  it('has one Save for name and access, enabled only once something changed', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Games' })
    const w = setup(talk, { manager: true })
    await flushPromises()
    const save = () => w.find<HTMLButtonElement>('[data-testid="talk-save"]').element
    expect(w.findAll('button').filter(b => b.text() === 'Save')).toHaveLength(1)
    expect(save().disabled).toBe(true)
    await w.find('[data-testid="talk-name"]').setValue('Board games')
    expect(save().disabled).toBe(false)
    await w.find('[data-testid="preset-roles"]').setValue(true)
    await w.find('[data-testid="preset-role-list"] input').setValue(true)
    await w.find('[data-testid="talk-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${talk.id}`, expect.objectContaining({ method: 'PATCH', json: expect.objectContaining({ name: 'Board games' }) }))
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${talk.id}/overwrites`, expect.objectContaining({ method: 'PUT' }))
  })

  it('titles the section "Limit" for the leader of a temporary Talk', async () => {
    const w = setup(channelFixture({ type: 'voice', name: 'Talk 1', is_temp: true, leader_id: me }), { manager: false })
    await flushPromises()
    expect(w.find('legend').text()).toBe('Limit')
  })

  it('hides the lobby switch without the feature', async () => {
    const w = setup(channelFixture({ type: 'voice' }), { manager: true })
    useServerFeaturesStore().list = []
    await flushPromises()
    expect(w.find('[data-testid="talk-lobby"]').exists()).toBe(false)
  })

  it('writes "Only these roles" as the ordinary overwrites', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Games' })
    const w = setup(talk, { manager: true })
    await flushPromises()
    await w.find('[data-testid="preset-roles"]').setValue(true)
    expect(w.find<HTMLButtonElement>('[data-testid="talk-save"]').element.disabled).toBe(true)
    await w.find('[data-testid="preset-role-list"] input').setValue(true)
    await w.find('[data-testid="talk-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${talk.id}/overwrites`, {
      method: 'PUT',
      json: { overwrites: [{ type: 'role', id: everyone.id, allow: 0, deny: JOIN }, { type: 'role', id: staff.id, allow: JOIN, deny: 0 }] }
    })
  })

  it('writes "Everyone" by lifting the denial and shows what is set now', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Games' })
    const w = setup(talk, { manager: true }, [{ type: 'role', id: everyone.id, allow: 0, deny: JOIN }, { type: 'role', id: staff.id, allow: JOIN, deny: 0 }])
    await flushPromises()
    expect(w.find<HTMLInputElement>('[data-testid="preset-roles"]').element.checked).toBe(true)
    await w.find('[data-testid="preset-everyone"]').setValue(true)
    await w.find('[data-testid="talk-save"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${talk.id}/overwrites`, { method: 'PUT', json: { overwrites: [] } })
  })

  it('tells a manager that a temporary Talk follows its lobby instead of offering access presets', async () => {
    const w = setup(channelFixture({ type: 'voice', name: 'Talk 2', is_temp: true, leader_id: fixtureId(9) }), { manager: true })
    await flushPromises()
    expect(w.find('[data-testid="preset-roles"]').exists()).toBe(false)
    expect(w.text()).toContain('follows its lobby')
  })

  it('switches the lobby on through the channel route', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Create a Talk' })
    const w = setup(talk, { manager: true })
    await flushPromises()
    await w.find('[data-testid="talk-lobby"]').setValue(true)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${talk.id}`, expect.objectContaining({ method: 'PATCH', json: { is_lobby: true } }))
  })

  it('does not send a stale name when the Talk was renamed elsewhere while the dialog is open', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Games' })
    const w = setup(talk, { manager: true })
    await flushPromises()
    await w.setProps({ channel: { ...talk, name: 'Renamed by someone else' } })
    await w.find('[data-testid="preset-roles"]').setValue(true)
    await w.find('[data-testid="preset-role-list"] input').setValue(true)
    await w.find('[data-testid="talk-save"]').trigger('click')
    await flushPromises()
    const patches = apiMock.mock.calls.filter(([, o]) => o?.method === 'PATCH')
    expect(patches).toHaveLength(0)
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${talk.id}/overwrites`, expect.objectContaining({ method: 'PUT' }))
  })

  it('keeps Save off and the presets locked when the current overwrites could not be read', async () => {
    const talk = channelFixture({ type: 'voice', name: 'Games' })
    const w = setup(talk, { manager: true })
    let fail = true
    apiMock.mockImplementation(async (path, options) => {
      if (path.endsWith('/overwrites')) {
        if (options?.method === 'PUT') return undefined
        if (fail) throw new Error('boom')
        return { overwrites: [] }
      }
      return talk
    })
    await w.unmount()
    const w2 = mount(TalkSettingsDialog, { props: { channel: talk } })
    await flushPromises()
    expect(w2.find('[data-testid="talk-presets"]').attributes('disabled')).toBeDefined()
    expect(w2.find<HTMLButtonElement>('[data-testid="talk-save"]').element.disabled).toBe(true)
    fail = false
    await w2.find('[data-testid="preset-retry"]').trigger('click')
    await flushPromises()
    expect(w2.find('[data-testid="talk-presets"]').attributes('disabled')).toBeUndefined()
    expect(w2.find('[data-testid="preset-retry"]').exists()).toBe(false)
    expect(apiMock.mock.calls.some(([, o]) => o?.method === 'PUT')).toBe(false)
  })
})
