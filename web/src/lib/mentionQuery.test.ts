import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect } from 'vitest'
import { findMentionQuery, suggestMentions, applyMention } from './mentionQuery'

const members = [
  userFixture({ id: '1', username: 'herzog', display_name: 'Herzog' }),
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
    expect(got).toEqual(['herzog', 'anna', 'here'])
    expect(suggestMentions('m', members, { selfId: '2' }).map(i => i.username)).toEqual(['moritz'])
  })
  it('offers @here and @all for an empty query, never the reader', () => {
    const got = suggestMentions('', members, { selfId: '1' }).map(i => i.username)
    expect(got).not.toContain('herzog')
    expect(got.slice(-2)).toEqual(['here', 'all'])
  })
  it('matches a later word of the display name', () => {
    expect(suggestMentions('muster', members).map(i => i.username)).toEqual(['max'])
  })
})

describe('applyMention', () => {
  it('splices the name in and places the caret after it', () => {
    expect(applyMention('hi @he', 3, 6, 'herzog')).toEqual({ text: 'hi @herzog ', caret: 11 })
    expect(applyMention('hi @he there', 3, 6, 'herzog')).toEqual({ text: 'hi @herzog there', caret: 10 })
    expect(applyMention('@hezog', 0, 3, 'herzog')).toEqual({ text: '@herzog ', caret: 8 })
  })
})

it('ignores a caret outside the current draft', () => {
  expect(findMentionQuery('@draft', 100)).toBeNull()
})
