import { describe, expect, it } from 'vitest'
import { inviteCodeFromPath, inviteLink, isInvitePath } from './inviteLink'

describe('inviteCodeFromPath', () => {
  it('reads the code of an invite path', () => {
    expect(inviteCodeFromPath('/invite/abc234xyz789')).toBe('abc234xyz789')
    expect(inviteCodeFromPath('/invite/Team_Night-2026/')).toBe('Team_Night-2026')
  })

  it('ignores everything that is not an invite path', () => {
    for (const path of ['/', '/invite', '/invite/', '/invite/abc', '/invite/has space', '/invite/a/b', '/c/abcdef123', '/x/invite/abcdef123', `/invite/${'a'.repeat(65)}`]) {
      expect(inviteCodeFromPath(path)).toBe('')
      expect(isInvitePath(path)).toBe(false)
    }
    expect(isInvitePath('/invite/abcdef123')).toBe(true)
  })
})

describe('inviteLink', () => {
  it('builds the shareable link', () => {
    expect(inviteLink('https://talk.example', 'abc234xyz789')).toBe('https://talk.example/invite/abc234xyz789')
  })
})
