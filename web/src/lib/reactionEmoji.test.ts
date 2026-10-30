import { describe, expect, it } from 'vitest'
import { customReaction, resolveReaction } from './reactionEmoji'
import type { CustomEmoji } from './customEmoji'

const party: CustomEmoji = { id: '11111111-2222-3333-4444-555555555555', name: 'party', animated: false, url: '/e/party.png' }
const byName = new Map([['party', party]])

describe('customReaction', () => {
  it('turns :name: of a custom emoji into its token', () => {
    expect(customReaction(':party:', byName)).toBe('<:party:11111111-2222-3333-4444-555555555555>')
    expect(customReaction(' :party: ', byName)).toBe('<:party:11111111-2222-3333-4444-555555555555>')
  })

  it('leaves unicode emoji, tokens and unknown names alone', () => {
    for (const e of ['🎉', '👍🏽', '👨‍👩‍👧', '🏳️‍🌈', '🇩🇪', '1️⃣', '<:party:11111111-2222-3333-4444-555555555555>', ':nope:']) {
      expect(customReaction(e, byName)).toBe(e)
    }
  })
})

describe('resolveReaction', () => {
  it('resolves standard shortcodes and prefers the custom emoji', async () => {
    expect(await resolveReaction(':tada:', byName)).toBe('🎉')
    expect(await resolveReaction(':partying_face:', byName)).toBe('🥳')
    expect(await resolveReaction(':party:', byName)).toBe('<:party:11111111-2222-3333-4444-555555555555>')
    expect(await resolveReaction('🥳', byName)).toBe('🥳')
    expect(await resolveReaction(':definitely_not_an_emoji:', byName)).toBe(':definitely_not_an_emoji:')
  })
})
