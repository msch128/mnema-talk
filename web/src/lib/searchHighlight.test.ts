import { describe, expect, it } from 'vitest'
import { splitHighlight } from './searchHighlight'

describe('splitHighlight', () => {
  it('splits around case-insensitive matches of any term', () => {
    expect(splitHighlight('Park near the park', ['PARK'])).toEqual([
      { text: 'Park', match: true }, { text: ' near the ', match: false }, { text: 'park', match: true },
    ])
    expect(splitHighlight('a b c', ['a', 'c'])).toEqual([
      { text: 'a', match: true }, { text: ' b ', match: false }, { text: 'c', match: true },
    ])
  })

  it('merges overlapping terms', () => {
    expect(splitHighlight('banana', ['ban', 'nan'])).toEqual([{ text: 'banan', match: true }, { text: 'a', match: false }])
  })

  it('returns the text untouched without terms or matches, and nothing for empty text', () => {
    expect(splitHighlight('hello', [])).toEqual([{ text: 'hello', match: false }])
    expect(splitHighlight('hello', [''])).toEqual([{ text: 'hello', match: false }])
    expect(splitHighlight('hello', ['xyz'])).toEqual([{ text: 'hello', match: false }])
    expect(splitHighlight('', ['a'])).toEqual([])
  })

  it('treats markup and regex characters as plain text', () => {
    expect(splitHighlight('a <b>(x)</b>', ['(x)', '<b>'])).toEqual([
      { text: 'a ', match: false }, { text: '<b>(x)', match: true }, { text: '</b>', match: false },
    ])
  })

  it('does not misplace matches when lower-casing changes the length', () => {
    expect(splitHighlight('İstanbul', ['stan'])).toEqual([{ text: 'İstanbul', match: false }])
  })
})
