import { describe, expect, it } from 'vitest'
import { playerSrc, safeColor, safeHttpUrl } from './videoEmbed'

describe('playerSrc', () => {
  it('turns on autoplay for the two allowed players', () => {
    expect(playerSrc({ provider: 'youtube', url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ' }))
      .toBe('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1')
    expect(playerSrc({ provider: 'youtube', url: 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=90' }))
      .toBe('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=90&autoplay=1')
    expect(playerSrc({ provider: 'vimeo', url: 'https://player.vimeo.com/video/76979871?dnt=1&h=8272103f6e' }))
      .toBe('https://player.vimeo.com/video/76979871?dnt=1&h=8272103f6e&autoplay=1')
  })

  it.each([
    ['youtube', 'https://www.youtube.com/embed/dQw4w9WgXcQ'],
    ['youtube', 'https://www.youtube-nocookie.com.evil.example/embed/dQw4w9WgXcQ'],
    ['youtube', 'http://www.youtube-nocookie.com/embed/dQw4w9WgXcQ'],
    ['youtube', 'https://www.youtube-nocookie.com/embed/short'],
    ['youtube', 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?x=1'],
    ['youtube', 'https://player.vimeo.com/video/76979871?dnt=1'],
    ['vimeo', 'https://player.vimeo.com/video/76979871'],
    ['vimeo', 'https://player.vimeo.com/video/76979871?dnt=1&h=ZZZZZZ'],
    ['vimeo', 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ'],
    ['vimeo', 'javascript:alert(1)'],
    ['dailymotion', 'https://www.dailymotion.com/embed/video/x1']
  ] as const)('refuses %s player %s', (provider, url) => {
    expect(playerSrc({ provider, url } as never)).toBeNull()
  })

  it('has no player without an embed', () => {
    expect(playerSrc(undefined)).toBeNull()
  })
})

describe('safeHttpUrl and safeColor', () => {
  it('keeps only http(s) URLs', () => {
    expect(safeHttpUrl('https://example.org/a b')).toBe('https://example.org/a%20b')
    expect(safeHttpUrl('javascript:alert(1)')).toBe('')
    expect(safeHttpUrl('data:text/html,x')).toBe('')
    expect(safeHttpUrl('/relative')).toBe('')
    expect(safeHttpUrl(undefined)).toBe('')
  })

  it('keeps only #rrggbb colours', () => {
    expect(safeColor('#1b2838')).toBe('#1b2838')
    for (const bad of ['red', '#fff', 'url(x)', '#1B2838', 'expression(1)', '']) expect(safeColor(bad)).toBe('')
  })
})
