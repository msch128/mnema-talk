import { describe, expect, it } from 'vitest'
import { hrefHost, needsConfirm, safeHref } from './link'

describe('safeHref', () => {
  it.each(['https://example.com', 'http://example.com/a?b=1#c', 'HTTPS://EXAMPLE.COM', 'mailto:a@example.com'])('accepts %s', href => {
    expect(safeHref(href)).toBe(href)
  })

  it.each([
    'javascript:alert(1)', 'data:text/html,x', 'file:///etc/passwd', 'ftp://example.com', '//example.com', 'example.com', 'https://', 'mailto:',
    'https://exa mple.com', 'https://example.com/"x', 'https://example.com/<x>', 'https://example.com/\u0000', 'https://example.com/\\x', 'https:///x'
  ])('refuses %j', href => {
    expect(safeHref(href)).toBeNull()
  })
})

describe('hrefHost', () => {
  it('reads the host the browser would connect to, not what the text before "@" suggests', () => {
    expect(hrefHost('https://trusted.example@evil.test/x')).toBe('evil.test')
    expect(hrefHost('https://Example.COM:8443/x')).toBe('example.com')
    expect(hrefHost('mailto:a%40example.com')).toBe('a@example.com')
    expect(hrefHost('not a url')).toBe('')
  })
})

describe('needsConfirm', () => {
  it.each([
    ['Click here', 'https://example.com', true],
    ['example.com', 'https://example.com/x', false],
    ['https://example.com/other', 'https://example.com/x', false],
    ['www.example.com', 'https://example.com', false],
    ['example.com', 'https://www.example.com', false],
    ['example.com:8443/x', 'https://example.com:8443/y', false],
    ['example.com', 'https://evil.test', true],
    ['example.com.evil.test', 'https://example.com', true],
    ['https://trusted.test', 'https://trusted.test@evil.test/', true],
    ['a@example.com', 'mailto:a@example.com', false],
    ['mail me', 'mailto:a@example.com', true],
    ['v1.2.3 notes', 'https://example.com', true]
  ])('%j -> %s is %s', (label, href, expected) => {
    expect(needsConfirm(label, href)).toBe(expected)
  })

  it('asks when the destination has no host', () => {
    expect(needsConfirm('x', 'nonsense')).toBe(true)
  })
})
