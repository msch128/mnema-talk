import { describe, expect, it } from 'vitest'
import { deriveSlug } from './wikiSlug'

// The same table as internal/wiki/slug_test.go: the browser and the server derive one address.
describe('deriveSlug', () => {
  it.each<[string, string]>([
    ['Raid guide', 'raid-guide'],
    ['  Größe & Ärger!  ', 'groesse-aerger'],
    ['Café – Menü (v2)', 'cafe-menue-v2'],
    ['C++ / Go', 'c-go'],
    ['日本語', 'page'],
    ['', 'page'],
    ['---', 'page'],
    ['Ünder_score and--dashes', 'uender-score-and-dashes'],
    ['Already-a-slug-123', 'already-a-slug-123'],
    ['naïve façade Łódź Šibenik ž', 'naive-facade-lodz-sibenik-z']
  ])('%j becomes %j', (title, slug) => {
    expect(deriveSlug(title)).toBe(slug)
  })

  it('cuts at 80 characters without a trailing hyphen', () => {
    const got = deriveSlug('abc '.repeat(40))
    expect(got.length).toBeLessThanOrEqual(80)
    expect(got.endsWith('-')).toBe(false)
  })
})
