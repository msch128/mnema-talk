import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'
import { resetMessageSoundGap } from './chat/messageSound'
import { channelFixture, messageFixture, readStateFixture, userFixture, fixtureId } from '../test-fixtures.fixture'

const sound = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: sound.play }))
vi.mock('../lib/api', async importOriginal => ({ ...(await importOriginal<typeof import('../lib/api')>()), api: vi.fn(async () => []) }))
vi.mock('../lib/platform', async importOriginal => {
  const real = await importOriginal<typeof import('../lib/platform')>()
  return { ...real, platform: { ...real.platform, notificationPermission: () => 'denied', notify: vi.fn() } }
})

const ME = fixtureId(1)
const OTHER = fixtureId(9)
const open = channelFixture({ id: fixtureId(2), type: 'text' })
const side = channelFixture({ id: fixtureId(3), type: 'text' })
const incoming = (over: Parameters<typeof messageFixture>[0] = {}) =>
  messageFixture({ id: fixtureId(Math.floor(Math.random() * 1e6)), channel_id: side.id, user_id: OTHER, ...over })

let chat: ReturnType<typeof useChatStore>

beforeEach(() => {
  sound.play.mockReset()
  resetMessageSoundGap()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: ME, username: 'me', presence: 'online' })
  chat = useChatStore()
  chat.activeChannel = open
  chat.readStates = {
    [open.id]: readStateFixture({ channel_id: open.id }),
    [side.id]: readStateFixture({ channel_id: side.id })
  }
  vi.spyOn(document, 'hasFocus').mockReturnValue(true)
})
afterEach(() => vi.restoreAllMocks())

describe('message sounds', () => {
  it('plays the message sound for a mention in another channel, but not for plain chatter', () => {
    chat.handleWSEvent({ type: 'message_create', payload: incoming({ content: 'hello' }) })
    expect(sound.play).not.toHaveBeenCalled()
    chat.handleWSEvent({ type: 'message_create', payload: incoming({ content: 'hey @me', mentions: [ME] }) })
    expect(sound.play).toHaveBeenCalledWith('message')
  })

  it('plays for every message when the member chose all, once per burst', () => {
    useVoiceStore().messageSounds = 'all'
    chat.handleWSEvent({ type: 'message_create', payload: incoming() })
    chat.handleWSEvent({ type: 'message_create', payload: incoming() })
    expect(sound.play).toHaveBeenCalledTimes(1)
  })

  it('stays silent for own messages, the open channel, do not disturb and the off choice', () => {
    useVoiceStore().messageSounds = 'all'
    chat.handleWSEvent({ type: 'message_create', payload: incoming({ user_id: ME }) })
    chat.handleWSEvent({ type: 'message_create', payload: incoming({ channel_id: open.id }) })
    expect(sound.play).not.toHaveBeenCalled()

    useAuthStore().user = userFixture({ id: ME, presence: 'dnd' })
    chat.handleWSEvent({ type: 'message_create', payload: incoming() })
    expect(sound.play).not.toHaveBeenCalled()

    useAuthStore().user = userFixture({ id: ME, presence: 'online' })
    useVoiceStore().messageSounds = 'off'
    chat.handleWSEvent({ type: 'message_create', payload: incoming() })
    expect(sound.play).not.toHaveBeenCalled()
  })

  it('respects a muted channel', () => {
    useVoiceStore().messageSounds = 'all'
    chat.readStates = { ...chat.readStates, [side.id]: readStateFixture({ channel_id: side.id, notify_level: 'mute' }) }
    chat.handleWSEvent({ type: 'message_create', payload: incoming() })
    expect(sound.play).not.toHaveBeenCalled()
  })
})
