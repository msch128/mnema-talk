import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useEventRoles } from './useEventRoles'
import { useEventsStore } from '../stores/events'
import { blankDraft } from '../lib/eventRoleDrafts'
import { eventWithRoles } from '../eventRoles.fixture'
import { setLocale } from '../i18n'

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
})
