import { describe, expect, it } from 'vitest'
import { charCount, clampChars } from './textLength'

const family = '👨‍👩‍👧‍👦'
const flag = '🇩🇪'
const wave = '👋🏽'
const accent = 'é'

describe('charCount', () => {
  it.each([
    ['ascii', 'hello', 5],
    ['cjk', '你好世界', 4],
    ['emoji are one point, two UTF-16 units', '😂😂😂', 3],
    ['zwj family is seven points', family, 7],
    ['flag is two points', flag, 2],
    ['skin tone is two points', wave, 2],
    ['combining mark is its own point', accent, 2],
    ['empty', '', 0],
  ])('%s', (_name, text, want) => {
    expect(charCount(text)).toBe(want)
  })

  it('differs from String.length for astral characters', () => {
    expect('😂'.length).toBe(2)
    expect(charCount('😂')).toBe(1)
  })
})

describe('clampChars', () => {
  it.each([
    ['fits', 'hello', 5, 'hello'],
    ['ascii cut', 'hello', 3, 'hel'],
    ['zero', 'hello', 0, ''],
    ['cjk', '你好世界', 2, '你好'],
    ['24 emoji fit a limit of 24', '😂'.repeat(24), 24, '😂'.repeat(24)],
    ['emoji cut at a point, not a unit', '😂'.repeat(30), 24, '😂'.repeat(24)],
    ['zwj family kept whole', `ab${family}`, 9, `ab${family}`],
    ['zwj family dropped when it does not fit', `ab${family}c`, 5, 'ab'],
    ['skin tone stays with its base', `a${wave}`, 2, 'a'],
    ['skin tone kept whole', `a${wave}`, 3, `a${wave}`],
    ['combining mark stays with its base', `ab${accent}`, 3, 'ab'],
    ['combining mark kept whole', `ab${accent}`, 4, `ab${accent}`],
    ['flag pair not split', `a${flag}`, 2, 'a'],
    ['flag pair kept', `a${flag}b`, 3, `a${flag}`],
    ['two flags cut between them', flag + flag, 2, flag],
  ])('%s', (_name, text, max, want) => {
    expect(clampChars(text, max)).toBe(want)
  })

  it('never returns a lone surrogate', () => {
    const cut = clampChars('😂😂😂', 2)
    expect(cut).toBe('😂😂')
    expect(/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/.test(cut)).toBe(false)
  })
})
