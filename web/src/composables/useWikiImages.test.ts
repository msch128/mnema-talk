import { describe, expect, it } from 'vitest'
import { altFromName } from './useWikiImages'

describe('altFromName', () => {
  it.each<[string, string]>([
    ['Site map.png', 'Site map'],
    ['archive.tar.gz', 'archive.tar'],
    ['we[ird]\nname.jpg', 'we ird name'],
    ['  spaced   out .gif', 'spaced out'],
    ['noextension', 'noextension'],
    [`${'a'.repeat(100)}.png`, 'a'.repeat(60)],
    ['.png', '']
  ])('%j becomes %j', (name, alt) => {
    expect(altFromName(name)).toBe(alt)
  })
})
