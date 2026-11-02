import { describe, it, expect } from 'vitest'
import { draftsOf, blankDraft, withTemplate, inputsOf, MAX_EVENT_ROLES } from './eventRoleDrafts'

const role = (name: string, extra = {}) => ({ id: `id-${name}`, name, position: 0, confirmed: 2, waitlist: 1, ...extra })

describe('event role drafts', () => {
  it('turns server roles into rows that remember their members', () => {
    const [a, b] = draftsOf([role('Tank', { capacity: 2, emoji: '🛡️' }), role('DPS')])
    expect(a).toMatchObject({ id: 'id-Tank', name: 'Tank', emoji: '🛡️', capacity: 2, members: 3 })
    expect(b).toMatchObject({ capacity: null, emoji: '' })
    expect(a!.key).not.toBe(b!.key)
  })

  it('sends trimmed rows, keeping ids and leaving out empty emoji and unlimited capacity', () => {
    const drafts = [{ ...blankDraft(), name: ' Tank ', emoji: ' 🛡️ ', capacity: 2 }, { ...draftsOf([role('DPS')])[0]!, name: 'DPS' }]
    expect(inputsOf(drafts)).toEqual([{ name: 'Tank', emoji: '🛡️', capacity: 2 }, { id: 'id-DPS', name: 'DPS' }])
  })

  it('can leave the ids out, for a template', () => {
    expect(inputsOf(draftsOf([role('DPS')]), false)).toEqual([{ name: 'DPS' }])
  })

  it('refuses rows the server would refuse', () => {
    const named = (name: string, capacity: number | null = null) => ({ ...blankDraft(), name, capacity })
    expect(inputsOf([named('')])).toBeNull()
    expect(inputsOf([named('x'.repeat(41))])).toBeNull()
    expect(inputsOf([named('Tank'), named('tank')])).toBeNull()
    expect(inputsOf([named('Tank', 0)])).toBeNull()
    expect(inputsOf([named('Tank', 1001)])).toBeNull()
    expect(inputsOf([named('Tank', 1.5)])).toBeNull()
    expect(inputsOf([])).toEqual([])
  })

  it('adds the roles of a template up to the limit, skipping names the list has', () => {
    const base = [{ ...blankDraft(), name: 'Tank' }]
    const out = withTemplate(base, [{ name: 'tank' }, { name: 'Heal', capacity: 1 }, { name: 'Heal' }])
    expect(out.map(d => d.name)).toEqual(['Tank', 'Heal'])
    const many = Array.from({ length: 20 }, (_, i) => ({ name: `r${i}` }))
    expect(withTemplate([], many)).toHaveLength(MAX_EVENT_ROLES)
  })
})
