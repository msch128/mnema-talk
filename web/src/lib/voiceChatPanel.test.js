import { describe, it, expect } from 'vitest'
import {
  loadVoiceChatOpen, saveVoiceChatOpen, voiceChatOpenFor, unreadBadge, VOICE_CHAT_OPEN_KEY
} from './voiceChatPanel'
import { parseRoute, resolveRoute } from './router'

function memoryStorage(data = {}) {
  return {
    data,
    getItem: k => (k in data ? data[k] : null),
    setItem: (k, v) => { data[k] = String(v) }
  }
}

describe('voice chat open state', () => {
  it('is closed by default and remembered per browser', () => {
    const s = memoryStorage()
    expect(loadVoiceChatOpen(s)).toBe(false)
    expect(saveVoiceChatOpen(true, s)).toBe(true)
    expect(s.data[VOICE_CHAT_OPEN_KEY]).toBe('1')
    expect(loadVoiceChatOpen(s)).toBe(true)
    saveVoiceChatOpen(false, s)
    expect(loadVoiceChatOpen(s)).toBe(false)
  })

  it('survives missing or throwing storage', () => {
    const broken = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') } }
    expect(loadVoiceChatOpen(broken)).toBe(false)
    expect(saveVoiceChatOpen(true, broken)).toBe(false)
    expect(loadVoiceChatOpen(null)).toBe(false)
  })

  it('a /v/:id/chat deep link opens it, /v/:id in the same Talk closes it, entering a Talk keeps it', () => {
    expect(voiceChatOpenFor({ routeShowChat: true, sameTalk: false, remembered: false })).toBe(true)
    expect(voiceChatOpenFor({ routeShowChat: true, sameTalk: true, remembered: false })).toBe(true)
    expect(voiceChatOpenFor({ routeShowChat: false, sameTalk: true, remembered: true })).toBe(false)
    expect(voiceChatOpenFor({ routeShowChat: false, sameTalk: false, remembered: true })).toBe(true)
    expect(voiceChatOpenFor({ routeShowChat: false, sameTalk: false, remembered: false })).toBe(false)
  })

  it('formats the unread badge', () => {
    expect(unreadBadge(0)).toBe('')
    expect(unreadBadge(undefined)).toBe('')
    expect(unreadBadge(7)).toBe('7')
    expect(unreadBadge(100)).toBe('99+')
  })
})

describe('voice chat routes', () => {
  it('parses the chat and a message in it', () => {
    expect(parseRoute('/v/v1/chat')).toEqual({ view: 'voice', channelId: 'v1', showChat: true, watching: true })
    expect(parseRoute('/v/v1/chat/m/m9')).toEqual({ view: 'voice', channelId: 'v1', showChat: true, messageId: 'm9', watching: true })
  })

  it('sends a message link of a voice channel to its Talk\'s chat', () => {
    const channels = [{ id: 'v1', type: 'voice' }]
    expect(resolveRoute({ view: 'chat', channelId: 'v1', messageId: 'm9' }, { channels }))
      .toEqual({ redirect: '/v/v1/chat/m/m9', reason: null })
    expect(resolveRoute({ view: 'chat', channelId: 'v1' }, { channels }))
      .toEqual({ redirect: '/v/v1', reason: null })
  })
})
