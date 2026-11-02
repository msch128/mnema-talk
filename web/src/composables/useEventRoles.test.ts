import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useEventRoles } from './useEventRoles'
import { useEventsStore } from '../stores/events'
import { blankDraft } from '../lib/eventRoleDrafts'
import { eventWithRoles } from '../eventRoles.fixture'
import { setLocale } from '../i18n'
import { ApiError } from '../lib/api'
import { roleFixture } from '../eventRoles.fixture'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('useEventRoles', () => {
  it('moves the members of removed roles first, with or without a target, then sets the list', async () => {
    const store = useEventsStore()
    const calls: string[] = []
    vi.spyOn(store, 'deleteRole').mockImplementation(async (_e, role, moveTo) => { calls.push(`${role}:${moveTo ?? '-'}`); return eventWithRoles() })
    vi.spyOn(store, 'setRoles').mockImplementation(async (_e, list) => { calls.push(`set ${list.length}`); return eventWithRoles() })
    await useEventRoles().save('e1', [{ ...blankDraft(), name: 'Tank' }], [{ id: 'r1', name: 'A', moveTo: 'r2' }, { id: 'r3', name: 'B', moveTo: null }])
    expect(calls).toEqual(['r1:r2', 'r3:-', 'set 1'])
  })

  it('refuses a list with an unnamed role before anything is sent', async () => {
    const store = useEventsStore()
    const setRoles = vi.spyOn(store, 'setRoles')
    await expect(useEventRoles().save('e1', [blankDraft()], [])).rejects.toThrow(/own name/)
    expect(setRoles).not.toHaveBeenCalled()
  })

  it('does not send a removal twice when the list failed to save, and hands the rest back for the retry', async () => {
    const store = useEventsStore()
    const deleted: string[] = []
    vi.spyOn(store, 'deleteRole').mockImplementation(async (_e, role) => { deleted.push(role); return eventWithRoles() })
    vi.spyOn(store, 'setRoles').mockRejectedValueOnce(new ApiError(409, 'CONFLICT', 'the role Tank has 3 confirmed members; remove 2 before lowering its capacity to 1')).mockResolvedValueOnce(eventWithRoles())
    vi.spyOn(store, 'fetchDetail').mockResolvedValue(eventWithRoles({ roles: [roleFixture(1, { confirmed: 3 })] }))
    const rows = [{ ...blankDraft(), id: roleFixture(1).id, name: 'Tank', members: 9 }, { ...blankDraft(), id: 'gone', name: 'Old', members: 4 }]
    let state: { rows: typeof rows; removals: { id: string; name: string; moveTo: string | null }[] } | null = null
    await expect(useEventRoles().save('e1', rows, [{ id: 'r1', name: 'A', moveTo: null }], s => { state = s as never })).rejects.toThrow()
    expect(state!.removals).toEqual([])
    expect(state!.rows[0]).toMatchObject({ id: roleFixture(1).id, members: 3 })
    expect(state!.rows[1]).not.toHaveProperty('id')
    await useEventRoles().save('e1', state!.rows, state!.removals)
    expect(deleted).toEqual(['r1'])
  })

  it('keeps the removals that were not reached and treats a role that is gone already as removed', async () => {
    const store = useEventsStore()
    const deleted: string[] = []
    vi.spyOn(store, 'deleteRole').mockImplementation(async (_e, role) => {
      deleted.push(role)
      if (role === 'r1') throw new ApiError(404, 'NOT_FOUND', 'role not found')
      if (role === 'r2') throw new ApiError(409, 'CONFLICT', 'busy')
      return eventWithRoles()
    })
    vi.spyOn(store, 'fetchDetail').mockRejectedValue(new Error('down'))
    let left: string[] = []
    const removals = [{ id: 'r1', name: 'A', moveTo: 'r2' }, { id: 'r2', name: 'B', moveTo: 'r3' }, { id: 'r3', name: 'C', moveTo: null }]
    await expect(useEventRoles().save('e1', [{ ...blankDraft(), name: 'Tank' }], removals, s => { left = s.removals.map(r => r.id) })).rejects.toThrow('busy')
    expect(deleted).toEqual(['r1', 'r2'])
    expect(left).toEqual(['r2', 'r3'])
  })
})
