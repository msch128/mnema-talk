import { describe, expect, it } from 'vitest'
import { groupSaved, matchesSaved, type SavedContext } from './savedGroups'
import { channelFixture, fixtureId, messageFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const general: SavedContext = { name: 'general', dm: false, voice: false }
const contexts: Record<string, SavedContext> = { [channelFixture().id]: general }
const contextOf = (id: string) => contexts[id]

const entry = (id: number, content: string, note = '', channel_id = channelFixture().id) => ({
  message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: true,
  message: messageFixture({ id: fixtureId(id), content, channel_id, display_name: 'Alice', username: 'alice' })
})
const gone = (id: number, note = '') => ({ message_id: fixtureId(id), note, saved_at: FIXTURE_TIMESTAMP, available: false })

describe('matchesSaved', () => {
  it('matches everything for an empty or blank query', () => {
    expect(matchesSaved(entry(1, 'hello'), '', contextOf)).toBe(true)
    expect(matchesSaved(entry(1, 'hello'), '   ', contextOf)).toBe(true)
  })

  it('needs every word, in any order, ignoring case, in text, note, author or channel', () => {
    const e = entry(1, 'The Wi-Fi password', 'router shelf')
    expect(matchesSaved(e, 'password wi-fi', contextOf)).toBe(true)
    expect(matchesSaved(e, 'SHELF', contextOf)).toBe(true)
    expect(matchesSaved(e, 'alice', contextOf)).toBe(true)
    expect(matchesSaved(e, 'general', contextOf)).toBe(true)
    expect(matchesSaved(e, 'password missing', contextOf)).toBe(false)
  })

  it('finds a message that is gone only by its note', () => {
    expect(matchesSaved(gone(2, 'for Friday'), 'friday', contextOf)).toBe(true)
    expect(matchesSaved(gone(2, 'for Friday'), 'alice', contextOf)).toBe(false)
  })

  it('does not match a channel it does not know the name of', () => {
    expect(matchesSaved(entry(3, 'hello', '', fixtureId(99)), 'general', contextOf)).toBe(false)
  })
})

describe('groupSaved', () => {
  it('keeps one ungrouped list in the recent order, and none when empty', () => {
    const list = [entry(1, 'a'), gone(2), entry(3, 'b')]
    const groups = groupSaved(list, 'recent', contextOf)
    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({ key: 'all', context: null, gone: false })
    expect(groups[0]!.entries.map(e => e.message_id)).toEqual(list.map(e => e.message_id))
    expect(groupSaved([], 'recent', contextOf)).toEqual([])
  })

  it('groups by channel in order of first appearance, with gone entries last', () => {
    const other = fixtureId(50)
    const groups = groupSaved([entry(1, 'a', '', other), gone(2), entry(3, 'b'), entry(4, 'c', '', other)], 'channel', contextOf)
    expect(groups.map(g => g.key)).toEqual([other, channelFixture().id, 'gone'])
    expect(groups[0]!.entries.map(e => e.message_id)).toEqual([fixtureId(1), fixtureId(4)])
    expect(groups[1]!.context).toEqual(general)
    expect(groups[2]).toMatchObject({ gone: true, context: null })
  })

  it('labels a channel it does not know with an empty name, and has no gone group when nothing is gone', () => {
    const groups = groupSaved([entry(1, 'a', '', fixtureId(77))], 'channel', contextOf)
    expect(groups).toHaveLength(1)
    expect(groups[0]!.context).toEqual({ name: '', dm: false, voice: false })
    expect(groupSaved([], 'channel', contextOf)).toEqual([])
  })

  it('puts an available entry without its message with the gone ones', () => {
    const broken = { ...gone(5), available: true }
    expect(groupSaved([broken], 'channel', contextOf)[0]).toMatchObject({ key: 'gone', gone: true })
  })
})
