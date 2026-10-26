import { describe, it, expect } from 'vitest'
import { channelPath, channelRef, dmPath, isUuid, postPath, postShortId, shortId, slugify, talkPath, textPath } from './routePaths'

const POST = '0f3a9c1e-1111-4222-8333-444455556666'

describe('slugify', () => {
  it('spells umlauts out and strips other accents', () => {
    expect(slugify('Größe & Ärger über Öl')).toBe('groesse-aerger-ueber-oel')
    expect(slugify('Café Crème')).toBe('cafe-creme')
    expect(slugify('Łódź Ørsted')).toBe('lodz-orsted')
  })

  it('keeps [a-z0-9-] only, collapses dashes and drops emoji', () => {
    expect(slugify('  Game --- Night 🎮  ')).toBe('game-night')
    expect(slugify('a__b..c')).toBe('a-b-c')
  })

  it('falls back when nothing usable is left', () => {
    expect(slugify('🎮🎮')).toBe('channel')
    expect(slugify('', 50, 'post')).toBe('post')
  })

  it('cuts at the limit without a trailing dash', () => {
    const slug = slugify(`${'a'.repeat(59)} bbbb`)
    expect(slug).toBe('a'.repeat(59))
    expect(slugify('x'.repeat(100), 50).length).toBe(50)
  })
})

describe('channel addresses', () => {
  it('uses the slug and falls back to the id', () => {
    expect(channelRef({ id: 'c1', slug: 'general' })).toBe('general')
    expect(channelRef({ id: 'c1' })).toBe('c1')
    expect(textPath({ id: 'c1', slug: 'general' })).toBe('/c/general')
    expect(textPath({ id: 'c1', slug: 'general' }, { messageId: 'm1' })).toBe('/c/general/m/m1')
    expect(textPath({ id: 'c1', slug: 'general' }, { threadId: 't1' })).toBe('/c/general/t/t1')
  })

  it('opens Talks under /v and their messages in the chat', () => {
    const talk = { id: 'v1', slug: 'lounge', type: 'voice' }
    expect(talkPath(talk)).toBe('/v/lounge')
    expect(talkPath(talk, { chat: true })).toBe('/v/lounge/chat')
    expect(channelPath(talk)).toBe('/v/lounge')
    expect(channelPath(talk, { messageId: 'm1' })).toBe('/v/lounge/chat/m/m1')
    expect(channelPath({ id: 'c1', slug: 'general', type: 'text' })).toBe('/c/general')
  })
})

describe('forum post addresses', () => {
  it('names the post by title and the first 8 characters of its id', () => {
    expect(postPath({ id: 'f1', slug: 'ideas' }, { id: POST, title: 'Neue Küche?' })).toBe('/c/ideas/p/neue-kueche-0f3a9c1e')
  })

  it('uses the full id route when another post shares the short id or there is no title', () => {
    const twin = { id: '0f3a9c1e-9999-4222-8333-444455556666' }
    expect(postPath({ id: 'f1', slug: 'ideas' }, { id: POST, title: 'T' }, [twin])).toBe(`/c/ideas/t/${POST}`)
    expect(postPath({ id: 'f1', slug: 'ideas' }, { id: POST })).toBe(`/c/ideas/t/${POST}`)
  })

  it('reads the short id back from an address or a full id', () => {
    expect(postShortId('neue-kueche-0f3a9c1e')).toBe('0f3a9c1e')
    expect(postShortId('0f3a9c1e')).toBe('0f3a9c1e')
    expect(postShortId(POST.toUpperCase())).toBe('0f3a9c1e')
    expect(postShortId('no-id-here')).toBeNull()
    expect(shortId(POST)).toBe('0f3a9c1e')
    expect(isUuid(POST)).toBe(true)
    expect(isUuid('general')).toBe(false)
  })
})

describe('conversation addresses', () => {
  const alice = { id: 'aaaaaaaa-0000-4000-8000-000000000001', group: false, peer: 'alice' }
  const group = { id: 'bbbbbbbb-0000-4000-8000-000000000002', group: true, peer: null }

  it('uses the other member for two and the short id for a group', () => {
    expect(dmPath(alice)).toBe('/dm/alice')
    expect(dmPath(alice, [], { messageId: 'm1' })).toBe('/dm/alice/m/m1')
    expect(dmPath(group)).toBe('/dm/g/bbbbbbbb')
  })

  it('takes the full id when another group shares the short id, and for a member called g', () => {
    const twin = { ...group, id: 'bbbbbbbb-ffff-4000-8000-000000000003' }
    expect(dmPath(group, [group, twin])).toBe(`/dm/g/${group.id}`)
    expect(dmPath({ ...alice, peer: 'g' })).toBe('/dm/g/aaaaaaaa')
    expect(dmPath({ ...alice, peer: 'a b' })).toBe('/dm/a%20b')
  })
})
