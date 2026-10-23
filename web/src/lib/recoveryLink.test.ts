import { afterEach, describe, expect, it } from 'vitest'
import { recoveryFromLocation, scrubRecoveryUrl } from './recoveryLink'

const TOKEN = 'A'.repeat(43)

describe('recovery links', () => {
  afterEach(() => window.history.replaceState(null, '', '/'))

  it('reads the token from the fragment of a confirmation or reset link', () => {
    expect(recoveryFromLocation('/verify-email', `#token=${TOKEN}`)).toEqual({ kind: 'verify', token: TOKEN })
    expect(recoveryFromLocation('/reset-password/', `#token=${TOKEN}`)).toEqual({ kind: 'reset', token: TOKEN })
  })

  it('treats a missing or malformed token as a dead link, not as another page', () => {
    for (const hash of ['', '#', '#token=', '#token=short', `#token=${TOKEN}x`, `#other=${TOKEN}`, `#token=${'!'.repeat(43)}`]) {
      expect(recoveryFromLocation('/reset-password', hash)).toEqual({ kind: 'reset', token: '' })
    }
  })

  it('ignores every other address', () => {
    expect(recoveryFromLocation('/', `#token=${TOKEN}`)).toBeNull()
    expect(recoveryFromLocation('/c/abc', '')).toBeNull()
    expect(recoveryFromLocation('/reset-password/extra', `#token=${TOKEN}`)).toBeNull()
  })

  it('removes the token from the address bar', () => {
    window.history.replaceState(null, '', `/reset-password#token=${TOKEN}`)
    scrubRecoveryUrl('/reset-password')
    expect(window.location.hash).toBe('')
    expect(window.location.pathname).toBe('/reset-password')
  })
})
