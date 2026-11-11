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

describe('vanity links', () => {
  it('reads the slug of /join/{name} in any case and refuses anything else', async () => {
    const { joinSlugFromPath, isInvitePath, vanityInviteCode, joinLink } = await import('./inviteLink')
    expect(joinSlugFromPath('/join/meinclan')).toBe('meinclan')
    expect(joinSlugFromPath('/join/MeinClan/')).toBe('meinclan')
    for (const bad of ['/join/ab', '/join/a_b', '/join/', '/join/x/y', '/joinx/abc', '/join/' + 'a'.repeat(33), '/join/%3Cb%3E']) {
      expect(joinSlugFromPath(bad), bad).toBe('')
    }
    expect(isInvitePath('/join/meinclan')).toBe(true)
    expect(isInvitePath('/c/general')).toBe(false)
    expect(vanityInviteCode('owls')).toBe('~owls')
    expect(joinLink('https://talk.example', 'owls')).toBe('https://talk.example/join/owls')
  })
})
