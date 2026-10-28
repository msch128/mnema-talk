import { describe, expect, it } from 'vitest'
import { dmClock, dmPreview, lastMessageOf, matchDms, splitDms } from './dmList'
import { dmTitle } from './dm'
import { setLocale } from '../i18n'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import { fixtureId, messageFixture } from '../test-fixtures.fixture'

const me = fixtureId(1)
const ada = dmMemberFixture(2, { display_name: 'Ada' })
const bo = dmMemberFixture(3, { display_name: 'Bo' })
const pair = dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada] })
const group = dmFixture({ id: fixtureId(101), group: true, members: [dmMemberFixture(1), ada, bo] })

describe('splitDms', () => {
  it('puts favourites first in their saved order and drops closed conversations, except the open one', () => {
    const a = { ...pair, id: 'a', favourite_position: 1 }
    const b = { ...pair, id: 'b', favourite_position: 0 }
    const c = { ...pair, id: 'c', hidden: true }
    const d = { ...pair, id: 'd', hidden: true }
    const e = { ...pair, id: 'e' }
    const { favourites, recent } = splitDms([a, c, e, b, d], 'd')
    expect(favourites.map(x => x.id)).toEqual(['b', 'a'])
    expect(recent.map(x => x.id)).toEqual(['e', 'd'])
  })
})

describe('dmPreview', () => {
  it('says nothing for a conversation without messages', () => {
    expect(dmPreview(pair, me)).toEqual({ kind: 'none', text: '', sender: '' })
  })

  it('marks the callers own message and names the speaker only in a group', () => {
    const mine = { ...group, last_message: { user_id: me, text: '**hi** there', has_attachment: false, has_poll: false } }
    expect(dmPreview(mine, me)).toEqual({ kind: 'text', text: 'hi there', sender: 'me' })
    const theirs = { ...group, last_message: { user_id: ada.id, text: 'hello', has_attachment: false, has_poll: false } }
    expect(dmPreview(theirs, me).sender).toBe('Ada')
    const inPair = { ...pair, last_message: { user_id: ada.id, text: 'hello', has_attachment: false, has_poll: false } }
    expect(dmPreview(inPair, me).sender).toBe('')
  })

  it('uses a placeholder for attachments and polls', () => {
    const file = { ...pair, last_message: { user_id: ada.id, text: '', has_attachment: true, has_poll: false } }
    expect(dmPreview(file, me).kind).toBe('attachment')
    const poll = { ...pair, last_message: { user_id: ada.id, text: 'Lunch?', has_attachment: false, has_poll: true } }
    expect(dmPreview(poll, me)).toMatchObject({ kind: 'poll', text: '' })
  })
})

describe('lastMessageOf', () => {
  it('summarises a message the way the server does', () => {
    expect(lastMessageOf(messageFixture({ user_id: ada.id, content: 'x'.repeat(300) }))).toMatchObject({ user_id: ada.id, has_attachment: false, has_poll: false })
    expect(lastMessageOf(messageFixture({ content: 'x'.repeat(300) })).text).toHaveLength(200)
  })
})

describe('dmClock', () => {
  const now = Date.parse('2026-03-18T15:00:00')
  it('reads as the clock today, the weekday this week and the date before', () => {
    expect(dmClock(null, 'en', now)).toBe('')
    expect(dmClock('nope', 'en', now)).toBe('')
    expect(dmClock('2026-03-18T09:05:00', 'en', now)).toMatch(/^9:05/)
    expect(dmClock('2026-03-18T13:13:00', 'en-US', now)).toMatch(/^1:13/)
    expect(dmClock('2026-03-16T09:05:00', 'en', now)).toBe('Mon')
    expect(dmClock('2026-01-02T09:05:00', 'en', now)).toBe('Jan 2')
  })
})

describe('matching', () => {
  it('finds conversations by a member’s name or username, ignoring case', () => {
    expect(matchDms([pair, group], me, ' ADA ').map(d => d.id)).toEqual([pair.id, group.id])
    expect(matchDms([pair, group], me, 'bo').map(d => d.id)).toEqual([group.id])
    expect(matchDms([pair, group], me, '')).toEqual([])
    expect(matchDms([pair, group], me, 'member1')).toEqual([])
  })
})

describe('deleted members', () => {
  it('are named in the interface language in the title and the preview', () => {
    setLocale('de')
    const gone = dmMemberFixture(4, { display_name: 'Deleted member', username: 'deleted-abc123' })
    const dm = dmFixture({ id: fixtureId(102), group: true, members: [dmMemberFixture(1), ada, gone], last_message: { user_id: gone.id, text: 'hi', has_attachment: false, has_poll: false } })
    expect(dmTitle(dm, me)).not.toContain('Deleted member')
    expect(dmPreview(dm, me).sender).not.toBe('Deleted member')
    setLocale('en')
    expect(dmPreview(dm, me).sender).toBe('Deleted member')
  })
})
