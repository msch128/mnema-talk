import { describe, it, expect } from 'vitest'
import { shouldPlayMessageSound, normalizeMessageSoundMode, type MessageSoundContext } from './messageSound'

const base: MessageSoundContext = {
  mode: 'mentions', isOwn: false, quiet: false, level: 'all', isDm: false, isMention: false,
  isDirectMention: false, threadSilenced: false, viewing: false, osNotified: false
}
const play = (over: Partial<MessageSoundContext>) => shouldPlayMessageSound({ ...base, ...over })

describe('shouldPlayMessageSound', () => {
  it('by default plays for direct messages and mentions only', () => {
    expect(play({})).toBe(false)
    expect(play({ isDm: true })).toBe(true)
    expect(play({ isMention: true })).toBe(true)
  })

  it('plays for every message of a channel set to all messages when asked to', () => {
    expect(play({ mode: 'all' })).toBe(true)
    expect(play({ mode: 'all', level: 'mentions' })).toBe(false)
    expect(play({ mode: 'all', level: 'mentions', isMention: true })).toBe(true)
  })

  it('is silent when switched off', () => {
    expect(play({ mode: 'off', isDm: true, isMention: true })).toBe(false)
  })

  it('follows mutes: only what is addressed by name, role or reply breaks one', () => {
    expect(play({ mode: 'all', level: 'mute' })).toBe(false)
    expect(play({ level: 'mute', isDm: true })).toBe(false)
    expect(play({ level: 'mute', isMention: true })).toBe(false)
    expect(play({ level: 'mute', isMention: true, isDirectMention: true })).toBe(true)
  })

  it('stays quiet for own messages, do not disturb and focus', () => {
    expect(play({ mode: 'all', isOwn: true })).toBe(false)
    expect(play({ mode: 'all', quiet: true, isDm: true })).toBe(false)
  })

  it('stays quiet for the conversation in front and for a thread the member is not in', () => {
    expect(play({ mode: 'all', viewing: true, isDm: true })).toBe(false)
    expect(play({ mode: 'all', threadSilenced: true })).toBe(false)
  })

  it('leaves the sound to the system when an OS notification was raised', () => {
    expect(play({ isDm: true, osNotified: true })).toBe(false)
  })
})

describe('normalizeMessageSoundMode', () => {
  it('keeps the known choices and falls back to mentions', () => {
    expect(normalizeMessageSoundMode('all')).toBe('all')
    expect(normalizeMessageSoundMode('off')).toBe('off')
    expect(normalizeMessageSoundMode(null)).toBe('mentions')
    expect(normalizeMessageSoundMode('loud')).toBe('mentions')
  })
})
