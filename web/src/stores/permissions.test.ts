import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import { usePermissionsStore } from './permissions'
import { useAuthStore } from './auth'
import { PERMISSION_BITS, DEFAULT_EVERYONE } from '../lib/permissions'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import type { Role } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const channel = fixtureId(2)
const other = fixtureId(3)
const role = (overrides: Partial<Role>): Role => ({
  id: fixtureId(10), name: 'Role', color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const info = (overrides: Record<string, unknown> = {}) => ({
  owner: false, permissions: DEFAULT_EVERYONE, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [],
  channels: [{ channel_id: channel, permissions: PERMISSION_BITS.view_channel | PERMISSION_BITS.send_messages }], ...overrides
})

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

describe('permissions store', () => {
  it('assumes the defaults until the server answers', () => {
    const store = usePermissionsStore()
    expect(store.loaded).toBe(false)
    expect(store.can('send_messages')).toBe(true)
    expect(store.can('manage_roles')).toBe(false)
    expect(store.canIn(channel, 'attach_files')).toBe(true)
    expect(store.canIn(channel, 'manage_messages')).toBe(false)
    expect(store.canOpenAdmin).toBe(false)
  })

  it('gives the Owner everything, known from the account', () => {
    useAuthStore().user = userFixture({ role: 'admin' })
    const store = usePermissionsStore()
    expect(store.isOwner).toBe(true)
    expect(store.can('administrator')).toBe(true)
    expect(store.canIn(other, 'manage_messages')).toBe(true)
    expect(store.canOpenAdmin).toBe(true)
    expect(store.outranks(role({ position: 99 }))).toBe(true)
    expect(store.outranksMember(other)).toBe(true)
  })

  it('follows the server once loaded', async () => {
    apiMock.mockResolvedValue(info({ permissions: PERMISSION_BITS.create_invites, top_position: 3 }))
    const store = usePermissionsStore()
    await store.load()
    expect(apiMock).toHaveBeenCalledWith('/api/permissions', { decode: expect.any(Function) })
    expect(store.loaded).toBe(true)
    expect(store.can('create_invites')).toBe(true)
    expect(store.can('send_messages')).toBe(false)
    expect(store.canOpenAdmin).toBe(true)
    expect(store.canIn(channel, 'send_messages')).toBe(true)
    expect(store.canIn(channel, 'attach_files')).toBe(false)
    expect(store.canIn(other, 'view_channel')).toBe(false) // a channel they cannot view is not listed
    expect(store.topPosition).toBe(3)
  })

  it('reports the effective upload limit once loaded', async () => {
    const store = usePermissionsStore()
    expect(store.maxUploadMb).toBeNull()
    apiMock.mockResolvedValue(info({ max_upload_mb: 7 }))
    await store.load()
    expect(store.maxUploadMb).toBe(7)
  })

  it('compares ranks of roles and members', async () => {
    apiMock.mockResolvedValueOnce(info({ role_ids: [fixtureId(11)], top_position: 5 }))
    const store = usePermissionsStore()
    store.roles = [role({ id: fixtureId(11), position: 5, color: 0xff0000 }), role({ id: fixtureId(12), position: 2, color: 0x00ff00 }), role({ id: fixtureId(13), position: 7 })]
    await store.load()
    expect(store.outranks(role({ position: 4 }))).toBe(true)
    expect(store.outranks(role({ position: 5 }))).toBe(false)
    store.setMemberRoles([{ id: other, role_ids: [fixtureId(12)] }, { id: fixtureId(4), role_ids: [fixtureId(13)] }, { id: fixtureId(5) }])
    expect(store.outranksMember(other)).toBe(true)
    expect(store.outranksMember(fixtureId(4))).toBe(false)
    expect(store.outranksMember(fixtureId(5))).toBe(true)
  })

  it('does not count a role members choose themselves towards a rank', async () => {
    apiMock.mockResolvedValueOnce(info({ role_ids: [fixtureId(11)], top_position: 5 }))
    const store = usePermissionsStore()
    store.roles = [role({ id: fixtureId(11), position: 5 }), role({ id: fixtureId(14), position: 9, self_assignable: true })]
    await store.load()
    store.setMemberRoles([{ id: other, role_ids: [fixtureId(14)] }])
    expect(store.outranksMember(other)).toBe(true)
  })

  it('colours names by the highest role that has a colour', async () => {
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    apiMock.mockResolvedValueOnce(info({ role_ids: [fixtureId(11)] }))
    const store = usePermissionsStore()
    store.roles = [role({ id: fixtureId(11), color: 0 }), role({ id: fixtureId(12), color: 0xff0000 }), role({ id: fixtureId(13), color: 0x00ff00 })]
    store.setMemberRoles([{ id: other, role_ids: [fixtureId(11), fixtureId(12), fixtureId(13)] }, { id: fixtureId(4), role_ids: ['unknown'] }])
    await store.load()
    expect(store.nameColor(other)).toBe('#ff0000')
    expect(store.nameColor(fixtureId(4))).toBe('')
    expect(store.nameColor(fixtureId(9))).toBe('')
    expect(store.nameColor(fixtureId(1))).toBe('') // the caller's own role has no colour
    expect(store.rolesOf(other).map(r => r.id)).toEqual([fixtureId(11), fixtureId(12), fixtureId(13)])
    expect(store.roleById(fixtureId(12))?.color).toBe(0xff0000)
  })

  it('loads roles and keeps the old state when a request fails', async () => {
    const store = usePermissionsStore()
    apiMock.mockResolvedValueOnce([role({ id: fixtureId(11) })])
    await store.loadRoles()
    expect(store.roles).toHaveLength(1)
    apiMock.mockRejectedValue(new Error('offline'))
    await store.loadRoles()
    await store.load()
    expect(store.roles).toHaveLength(1)
    expect(store.loaded).toBe(false)
  })

  it('drops an answer that a reset or a newer request overtook', async () => {
    const store = usePermissionsStore()
    let resolve: (value: unknown) => void = () => {}
    apiMock.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const pending = store.load()
    store.reset()
    resolve(info())
    await pending
    expect(store.loaded).toBe(false)
    store.setMemberRoles([{ id: other, role_ids: [fixtureId(11)] }])
    store.reset()
    expect(store.memberRoles).toEqual({})
  })

  it('reports a load as done only after the newest request has landed', async () => {
    const store = usePermissionsStore()
    let first: (value: unknown) => void = () => {}
    let second: (value: unknown) => void = () => {}
    apiMock.mockReturnValueOnce(new Promise(r => { first = r }))
    apiMock.mockReturnValueOnce(new Promise(r => { second = r }))
    // A caller waits for the permissions while the socket's refresh overtakes it.
    let done = false
    const waiting = store.load().then(() => { done = true })
    const refresh = store.load()
    first(info({ permissions: DEFAULT_EVERYONE }))
    await Promise.resolve()
    await Promise.resolve()
    expect(done).toBe(false)
    expect(store.loaded).toBe(false)
    second(info({ permissions: PERMISSION_BITS.manage_bots }))
    await Promise.all([waiting, refresh])
    expect(done).toBe(true)
    expect(store.can('manage_bots')).toBe(true)
    expect(store.canOpenAdmin).toBe(true)
  })
})
