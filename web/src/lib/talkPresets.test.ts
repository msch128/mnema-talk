import { describe, expect, it } from 'vitest'
import { currentPreset, presetEveryone, presetOnlyRoles, rolesLetIn } from './talkPresets'
import { PERMISSION_BITS } from './permissions'
import type { Overwrite } from '../types/domain'

const JOIN = PERMISSION_BITS.view_channel | PERMISSION_BITS.connect
const SPEAK = PERMISSION_BITS.speak
const everyone = 'role-everyone'

const role = (id: string, allow = 0, deny = 0): Overwrite => ({ type: 'role', id, allow, deny })
const member = (id: string, allow = 0, deny = 0): Overwrite => ({ type: 'member', id, allow, deny })

describe('"Only these roles"', () => {
  it('shuts @everyone out and lets the chosen roles in', () => {
    const out = presetOnlyRoles([], everyone, ['mods', 'crew'])
    expect(out).toEqual([role(everyone, 0, JOIN), role('mods', JOIN), role('crew', JOIN)])
  })

  it('keeps unrelated bits and entries, and drops the access of roles no longer chosen', () => {
    const before = [role(everyone, SPEAK, 0), role('old', JOIN | SPEAK), role('mods', 0, JOIN), member('ann', JOIN)]
    const out = presetOnlyRoles(before, everyone, ['mods'])
    expect(out).toContainEqual(role(everyone, SPEAK, JOIN))
    expect(out).toContainEqual(role('old', SPEAK))
    expect(out).toContainEqual(role('mods', JOIN))
    expect(out).toContainEqual(member('ann', JOIN))
  })

  it('does not mutate what it was given', () => {
    const before = [role(everyone)]
    presetOnlyRoles(before, everyone, ['mods'])
    expect(before).toEqual([role(everyone)])
  })
})

describe('"Everyone"', () => {
  it('lifts the denial for @everyone and the one-by-one access of roles', () => {
    const before = [role(everyone, 0, JOIN | SPEAK), role('mods', JOIN), member('ann', JOIN)]
    expect(presetEveryone(before, everyone)).toEqual([role(everyone, 0, SPEAK), member('ann', JOIN)])
  })

  it('leaves nothing behind when the Talk was open already', () => {
    expect(presetEveryone([], everyone)).toEqual([])
  })
})

describe('reading the current state', () => {
  it('says "roles" when @everyone is shut out, otherwise "everyone"', () => {
    expect(currentPreset([role(everyone, 0, JOIN)], everyone)).toBe('roles')
    expect(currentPreset([role(everyone, 0, PERMISSION_BITS.view_channel)], everyone)).toBe('roles')
    expect(currentPreset([role(everyone, 0, SPEAK)], everyone)).toBe('everyone')
    expect(currentPreset([], everyone)).toBe('everyone')
  })

  it('lists the roles that may both see and join', () => {
    const entries = [role(everyone, 0, JOIN), role('mods', JOIN), role('half', PERMISSION_BITS.view_channel), member('ann', JOIN)]
    expect(rolesLetIn(entries, everyone)).toEqual(['mods'])
  })
})
