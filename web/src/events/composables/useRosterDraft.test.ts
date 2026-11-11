import { describe, expect, it } from 'vitest'
import { nextTick, ref } from 'vue'
import { useRosterDraft } from './useRosterDraft'
import { blankDraft } from '../lib/eventRoleDrafts'
import { roleFixture } from '../../eventRoles.fixture'

const roles = (...names: string[]) => names.map((name, i) => roleFixture(i + 1, { name }))

describe('useRosterDraft', () => {
  it('differs from the saved roster only while the rows differ, ignoring surrounding blanks', () => {
    const saved = ref(roles('Tank', 'DPS'))
    const roster = useRosterDraft(() => saved.value)
    expect(roster.key.value).toBe(roster.savedKey.value)
    roster.rows.value[0]!.name = ' Tank '
    expect(roster.key.value).toBe(roster.savedKey.value)
    roster.rows.value[0]!.name = 'Tanks'
    expect(roster.key.value).not.toBe(roster.savedKey.value)
    roster.rows.value[0]!.name = 'Tank'
    expect(roster.key.value).toBe(roster.savedKey.value)
  })

  it('counts a pending removal and a reordering as changes, and reset brings the saved roster back', () => {
    const roster = useRosterDraft(() => roles('Tank', 'DPS'))
    roster.rows.value = [...roster.rows.value].reverse()
    expect(roster.key.value).not.toBe(roster.savedKey.value)
    roster.removals.value = [{ id: roster.rows.value[0]!.id!, name: 'DPS', moveTo: null }]
    roster.rows.value.push(blankDraft())
    roster.reset()
    expect(roster.rows.value.map(r => r.name)).toEqual(['Tank', 'DPS'])
    expect(roster.removals.value).toEqual([])
    expect(roster.key.value).toBe(roster.savedKey.value)
  })

  it('follows the event while untouched and keeps an edit when the event changes', async () => {
    const saved = ref(roles('Tank'))
    const roster = useRosterDraft(() => saved.value)
    saved.value = roles('Tank', 'Heal')
    await nextTick()
    expect(roster.rows.value.map(r => r.name)).toEqual(['Tank', 'Heal'])

    roster.rows.value[0]!.name = 'Mine'
    saved.value = roles('Tank', 'Heal', 'DPS')
    await nextTick()
    expect(roster.rows.value.map(r => r.name)).toEqual(['Mine', 'Heal'])
  })

  it('takes over what is left after a save that stopped half way', () => {
    const roster = useRosterDraft(() => roles('Tank'))
    const left = [{ id: 'gone', name: 'Old', moveTo: 'x' }]
    roster.sync({ rows: [blankDraft()], removals: left })
    expect(roster.rows.value).toHaveLength(1)
    expect(roster.removals.value).toEqual(left)
  })
})
