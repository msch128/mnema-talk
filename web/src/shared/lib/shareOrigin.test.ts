import { describe, expect, it } from 'vitest'
import { resolveShareOrigin } from './shareOrigin'
import { inviteLink } from './inviteLink'

const DESKTOP = 'https://mnema.localhost'

describe('resolveShareOrigin', () => {
  it('uses the public address instead of the desktop origin', () => {
    expect(resolveShareOrigin('https://chat.example.org', DESKTOP)).toBe('https://chat.example.org')
    expect(inviteLink(resolveShareOrigin('https://chat.example.org/', DESKTOP), 'abc123')).toBe('https://chat.example.org/invite/abc123')
  })

  it('falls back to the page origin while no address is announced', () => {
    expect(resolveShareOrigin('', 'http://localhost:5173')).toBe('http://localhost:5173')
    expect(resolveShareOrigin(undefined, DESKTOP)).toBe(DESKTOP)
  })

  it('ignores an address that is not http(s)', () => {
    expect(resolveShareOrigin('javascript:alert(1)', 'https://a.example')).toBe('https://a.example')
    expect(resolveShareOrigin('not a url', 'https://a.example')).toBe('https://a.example')
  })
})
