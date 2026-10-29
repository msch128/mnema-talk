import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect } from 'vitest'
import { findMentionQuery, suggestMentions, applyMention } from './mentionQuery'

const members = [
  userFixture({ id: '1', username: 'hera', display_name: 'Hera' }),
  userFixture({ id: '2', username: 'max', display_name: 'Max Mustermann' }),
  userFixture({ id: '3', username: 'moritz', display_name: 'Moritz' }),
  userFixture({ id: '4', username: 'anna', display_name: 'Anna Herz' })
]

describe('findMentionQuery', () => {
  it('finds the mention before the caret', () => {
    expect(findMentionQuery('hi @he', 6)).toEqual({ start: 3, query: 'he' })
    expect(findMentionQuery('@', 1)).toEqual({ start: 0, query: '' })
    expect(findMentionQuery('(@m', 3)).toEqual({ start: 1, query: 'm' })
  })
  it('ignores e-mail addresses and finished words', () => {
    expect(findMentionQuery('mail@max', 8)).toBeNull()
    expect(findMentionQuery('@max hi', 7)).toBeNull()
    expect(findMentionQuery('no mention', 10)).toBeNull()
  })
})

describe('suggestMentions', () => {
  it('ranks prefix matches first, then word and substring matches, then groups', () => {
    const got = suggestMentions('her', members).map(i => i.username)
    expect(got).toEqual(['hera', 'anna', 'here'])
    expect(suggestMentions('m', members, { selfId: '2' }).map(i => i.username)).toEqual(['moritz'])
  })
  it('offers @here and @all for an empty query, never the reader', () => {
    const got = suggestMentions('', members, { selfId: '1' }).map(i => i.username)
    expect(got).not.toContain('hera')
    expect(got.slice(-2)).toEqual(['here', 'all'])
  })
  it('matches a later word of the display name', () => {
    expect(suggestMentions('muster', members).map(i => i.username)).toEqual(['max'])
  })
})

describe('applyMention', () => {
  it('splices the name in and places the caret after it', () => {
    expect(applyMention('hi @he', 3, 6, 'hera')).toEqual({ text: 'hi @hera ', caret: 9 })
    expect(applyMention('hi @he there', 3, 6, 'hera')).toEqual({ text: 'hi @hera there', caret: 8 })
    expect(applyMention('@heza', 0, 3, 'hera')).toEqual({ text: '@hera ', caret: 6 })
  })
})

it('ignores a caret outside the current draft', () => {
  expect(findMentionQuery('@draft', 100)).toBeNull()
})

describe('role suggestions', () => {
  const role = (name: string) => ({
    id: name, name, color: 0, position: 1, permissions: 0, hoist: false, mentionable: true, is_everyone: false, max_upload_mb: null
  })
  it('offers matching roles after the members and before the groups', () => {
    const got = suggestMentions('m', members, { selfId: '1', roles: [role('Mods'), role('Crew'), role('Admins')] })
    expect(got.map(i => i.username)).toEqual(['max', 'moritz', 'Mods', 'Admins'])
    expect(got[2]?.role?.id).toBe('Mods')
    expect(suggestMentions('', members, { roles: [role('Mods')] }).map(i => i.username).slice(-3)).toEqual(['Mods', 'here', 'all'])
  })
  it('skips a role named like a member, since the member wins', () => {
    expect(suggestMentions('max', members, { roles: [role('Max')] }).filter(i => i.role)).toEqual([])
  })
  it('caps the roles and applies the name when picked', () => {
    const many = ['Aa', 'Ab', 'Ac', 'Ad', 'Ae'].map(role)
    expect(suggestMentions('a', [], { roles: many }).filter(i => i.role)).toHaveLength(3)
    expect(applyMention('hi @Dev', 3, 7, 'Dev Team')).toEqual({ text: 'hi @Dev Team ', caret: 13 })
  })
})
