import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../auth'
import { useVoiceStore } from '../voice'
import { createChatState, type Cx } from './state'
import { installActivityListeners, setMyPresence } from './presence'
import { IDLE_AFTER_MS } from '../../lib/presence'
import { userFixture, fixtureId } from '../../test-fixtures.fixture'

vi.mock('../../lib/api', async importOriginal => ({
  ...await importOriginal<typeof import('../../lib/api')>(),
  api: vi.fn(async (_url: string, opts: { json: { presence: string } }) => userFixture({ id: fixtureId(1), presence: opts.json.presence as 'away' }))
}))

const MIN = 60 * 1000
const me = userFixture({ id: fixtureId(1), presence: 'online' })

describe('automatic away', () => {
  let cx: Cx
  let sent: string[]
  const idleMessages = () => sent.map(s => JSON.parse(s) as { type: string; payload: { idle: boolean } }).filter(m => m.type === 'presence_idle').map(m => m.payload.idle)
  const isIdle = () => idleMessages().at(-1) === true
  const speak = (active: boolean) => cx.voiceStore.handleSpeakingEvent({ user_id: me.id, active })

  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-01-01T12:00:00Z'))
    setActivePinia(createPinia())
    const authStore = useAuthStore()
    authStore.user = { ...me }
    const cs = createChatState(() => {})
    sent = []
    cs.ws.value = { send: (data: string) => sent.push(data) } as unknown as WebSocket
    cs.isConnected.value = true
    cx = { cs, authStore, voiceStore: useVoiceStore() }
    cs.isIdle = false
    installActivityListeners(cx)
    sent.length = 0
  })
  afterEach(() => {
    clearTimeout(cx.cs.idleTimer)
    vi.useRealTimers()
  })

  function joinTalk(deafened = false) {
    cx.voiceStore.isConnected = true
    cx.voiceStore.currentChannelId = fixtureId(9)
    cx.voiceStore.isDeafened = deafened
  }

  it('goes away after the threshold without input or voice', () => {
    vi.advanceTimersByTime(IDLE_AFTER_MS - 1)
    expect(isIdle()).toBe(false)
    vi.advanceTimersByTime(1)
    expect(isIdle()).toBe(true)
  })

  it('stays online while speaking every 4 minutes in a Talk', () => {
    joinTalk()
    for (let i = 0; i < 8; i++) {
      speak(true)
      speak(false)
      vi.advanceTimersByTime(4 * MIN)
    }
    expect(idleMessages()).not.toContain(true)
  })

  it('does not go away for a silent listener who is not deafened', () => {
    joinTalk()
    vi.advanceTimersByTime(IDLE_AFTER_MS * 3)
    expect(idleMessages()).not.toContain(true)
  })

  it('counts push-to-talk presses as activity, also while deafened', () => {
    joinTalk(true)
    for (let i = 0; i < 6; i++) {
      cx.voiceStore.isPttPressed = true
      cx.voiceStore.isPttPressed = false
      vi.advanceTimersByTime(4 * MIN)
    }
    expect(idleMessages()).not.toContain(true)
    vi.advanceTimersByTime(IDLE_AFTER_MS)
    expect(isIdle()).toBe(true)
  })

  it('goes away when deafened and silent', () => {
    joinTalk(true)
    vi.advanceTimersByTime(IDLE_AFTER_MS)
    expect(isIdle()).toBe(true)
  })

  it('treats screen sharing and the camera as activity even when deafened', () => {
    joinTalk(true)
    cx.voiceStore.isScreenSharing = true
    vi.advanceTimersByTime(IDLE_AFTER_MS * 2)
    expect(idleMessages()).not.toContain(true)
    cx.voiceStore.isScreenSharing = false
    cx.voiceStore.isCameraOn = true
    vi.advanceTimersByTime(IDLE_AFTER_MS * 2)
    expect(idleMessages()).not.toContain(true)
  })

  it('wakes an idle member who joins a Talk', () => {
    vi.advanceTimersByTime(IDLE_AFTER_MS)
    expect(isIdle()).toBe(true)
    joinTalk()
    expect(idleMessages().at(-1)).toBe(false)
  })

  it('restores normal idle handling after leaving the Talk', () => {
    joinTalk()
    vi.advanceTimersByTime(IDLE_AFTER_MS * 2)
    expect(idleMessages()).not.toContain(true)
    cx.voiceStore.isConnected = false
    cx.voiceStore.currentChannelId = null
    vi.advanceTimersByTime(IDLE_AFTER_MS - 1)
    expect(idleMessages()).not.toContain(true)
    vi.advanceTimersByTime(1)
    expect(isIdle()).toBe(true)
  })

  it('leaves manually chosen statuses alone', async () => {
    for (const chosen of ['dnd', 'away', 'focus'] as const) {
      cx.cs.presenceById.value = { [me.id]: 'online' }
      await setMyPresence(cx, chosen)
      joinTalk()
      vi.advanceTimersByTime(IDLE_AFTER_MS * 2)
      speak(true)
      speak(false)
      expect(cx.cs.presenceById.value[me.id]).toBe(chosen)
      expect(cx.authStore.user?.presence).toBe(chosen)
    }
  })
})
