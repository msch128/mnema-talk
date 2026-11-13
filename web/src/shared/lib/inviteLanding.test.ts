import { describe, expect, it } from 'vitest'
import { inviteLandingRoute, rememberInviteLanding } from './inviteLanding'

const channels = [
  { id: 'c-text', type: 'text', slug: 'general' },
  { id: 'c-voice', type: 'voice', slug: 'lounge' },
  { id: 'c-bare', type: 'text' }
]

describe('inviteLandingRoute', () => {
  it('leads to the remembered channel when the member was sent it', () => {
    rememberInviteLanding('c-text')
    expect(inviteLandingRoute(channels)).toBe('/c/general')
    rememberInviteLanding('c-voice')
    expect(inviteLandingRoute(channels)).toBe('/v/lounge')
    rememberInviteLanding('c-bare')
    expect(inviteLandingRoute(channels)).toBe('/c/c-bare')
  })

  it('leaves the default place when the channel is not in the member’s list', () => {
    rememberInviteLanding('hidden')
    expect(inviteLandingRoute(channels)).toBeNull()
  })

  it('has nothing for an invite without a channel and forgets after one use', () => {
    rememberInviteLanding(undefined)
    expect(inviteLandingRoute(channels)).toBeNull()
    rememberInviteLanding('c-text')
    inviteLandingRoute(channels)
    expect(inviteLandingRoute(channels)).toBeNull()
  })
})
