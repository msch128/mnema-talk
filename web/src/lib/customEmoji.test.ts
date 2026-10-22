import { afterEach, beforeEach, describe, it, expect } from 'vitest'
import { emojiImage, emojiToken, encodeShortcodes, parseEmojiToken, plainEmojiText, setEmojiSource, type CustomEmoji } from './customEmoji'
import { findEmojiQuery, suggestEmoji, applyEmoji } from './emojiQuery'
import { markdownToPlain, renderMarkdown } from './markdown'

const ID = '11111111-2222-4333-8444-555555555555'
const parrot: CustomEmoji = { id: ID, name: 'party_parrot', animated: true, url: '/api/emoji/x/image?v=1', still_url: '/api/emoji/x/still?v=1' }
const byName = new Map([[parrot.name, parrot]])
const lookup = (id: string) => (id === ID ? parrot : undefined)

describe('custom emoji tokens', () => {
  it('writes, parses and flattens a token', () => {
    const token = emojiToken(parrot)
    expect(token).toBe(`<:party_parrot:${ID}>`)
    expect(parseEmojiToken(token)).toEqual({ name: 'party_parrot', id: ID })
    expect(parseEmojiToken(`x${token}`)).toBeNull()
    expect(plainEmojiText(`hi ${token}!`)).toBe('hi :party_parrot:!')
  })

  it('turns typed :name: of known emoji into tokens, but not inside code or unknown names', () => {
    const token = emojiToken(parrot)
    expect(encodeShortcodes('go :party_parrot: now', byName)).toBe(`go ${token} now`)
    expect(encodeShortcodes('`:party_parrot:` and ```\n:party_parrot:\n```', byName)).toBe('`:party_parrot:` and ```\n:party_parrot:\n```')
    expect(encodeShortcodes(':nobody: 12:30:45', byName)).toBe(':nobody: 12:30:45')
    expect(encodeShortcodes('plain', new Map())).toBe('plain')
  })
})

describe('drawing custom emoji', () => {
  const text = `a <:party_parrot:${ID}> b`
  let reduced = false
  beforeEach(() => setEmojiSource(lookup, () => reduced))
  afterEach(() => { reduced = false; setEmojiSource(() => undefined, () => false) })

  it('draws a known emoji as an image with its name as alt text', () => {
    const html = renderMarkdown(text)
    expect(html).toContain('<img class="md-emoji" src="/api/emoji/x/image?v=1" alt=":party_parrot:"')
    expect(html).toContain('loading="lazy"')
  })

  it('shows the still frame when reduced motion is preferred', () => {
    reduced = true
    expect(renderMarkdown(text)).toContain('/api/emoji/x/still?v=1')
    expect(emojiImage({ id: ID, name: 'flat', animated: false, url: parrot.url }, true).src).toBe(parrot.url)
  })

  it('degrades a deleted emoji to :name: text', () => {
    setEmojiSource(() => undefined, () => false)
    expect(renderMarkdown(text)).toContain('a :party_parrot: b')
  })

  it('never lets raw HTML through next to an emoji', () => {
    const html = renderMarkdown(`<img src=x onerror=alert(1)> <:party_parrot:${ID}>`)
    expect(html).toContain('&lt;img src=x')
    expect(html.match(/<img /g)).toHaveLength(1)
  })

  it('does not draw an emoji inside code, and shows :name: in previews', () => {
    expect(renderMarkdown('`<:party_parrot:' + ID + '>`')).not.toContain('<img')
    expect(markdownToPlain(`hi ${text}`)).toBe('hi a :party_parrot: b')
  })
})

describe('emoji autocomplete query', () => {
  it('finds a ":que" after white space once it has two characters', () => {
    expect(findEmojiQuery('hi :pa', 6)).toEqual({ start: 3, query: 'pa' })
    expect(findEmojiQuery(':p', 2)).toBeNull()
    expect(findEmojiQuery('time 12:30', 10)).toBeNull()
    expect(findEmojiQuery('no colon', 8)).toBeNull()
    expect(findEmojiQuery(null, 0)).toBeNull()
  })

  it('ranks prefix matches first and splices the choice in', () => {
    const list = [{ ...parrot, name: 'big_party' }, parrot]
    expect(suggestEmoji('part', list).map(e => e.name)).toEqual(['party_parrot', 'big_party'])
    expect(applyEmoji('hi :pa there', 3, 6, 'party_parrot')).toEqual({ text: 'hi :party_parrot: there', caret: 17 })
    expect(applyEmoji('hi :pa', 3, 6, 'x_y')).toEqual({ text: 'hi :x_y: ', caret: 9 })
  })
})
