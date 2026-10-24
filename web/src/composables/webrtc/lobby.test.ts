import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createRtcState, type Cx } from './state'
import { isLobby, joinViaLobby, lobbyMoved, lobbyRefused } from './lobby'
import { useToastStore } from '../../stores/toast'
import { setLocale } from '../../i18n'
import { fixtureId } from '../../test-fixtures.fixture'

const navigate = vi.fn()
const loadPermissions = vi.fn(async () => {})
vi.mock('../../stores/permissions', () => ({ usePermissionsStore: () => ({ load: loadPermissions }) }))
vi.mock('../../lib/router', () => ({ navigate: (...args: unknown[]) => navigate(...args) }))

const LOBBY = fixtureId(10)
const TALK = fixtureId(11)

function world(channels: { id: string; is_lobby?: boolean }[] = [{ id: LOBBY, is_lobby: true }, { id: TALK }]) {
  const st = createRtcState()
  const chat = {
    allChannels: channels,
    sendWSEvent: vi.fn(),
    fetchChannels: vi.fn(async () => {}),
    selectChannel: vi.fn()
  }
  const voice = { activeView: 'chat' }
  const cx = { voice, chat, st, hooks: { filterFailed: vi.fn() } } as unknown as Cx
  return { cx, chat, voice, st }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.useFakeTimers()
  navigate.mockReset()
  loadPermissions.mockClear()
})
afterEach(() => { vi.useRealTimers() })

describe('Create-a-Talk lobbies in the call layer', () => {
  it('knows a lobby by the server flag, not by name', () => {
    const { cx } = world([{ id: LOBBY, is_lobby: true }, { id: TALK }])
    expect(isLobby(cx, LOBBY)).toBe(true)
    expect(isLobby(cx, TALK)).toBe(false)
    expect(isLobby(cx, fixtureId(99))).toBe(false)
  })

  it('asks the lobby for a Talk and joins the one the server opened', async () => {
    const { cx, chat, voice } = world()
    const join = vi.fn(async () => {})
    const joined = joinViaLobby(cx, LOBBY, join)
    expect(chat.sendWSEvent).toHaveBeenCalledWith('voice_join', { channel_id: LOBBY })
    expect(join).not.toHaveBeenCalled()

    lobbyMoved(cx, { from_channel_id: LOBBY, channel_id: TALK })
    await joined
    expect(chat.fetchChannels).toHaveBeenCalled()
    expect(loadPermissions).toHaveBeenCalled()
    expect(chat.selectChannel).toHaveBeenCalledWith({ id: TALK })
    expect(voice.activeView).toBe('voice')
    expect(navigate).toHaveBeenCalledWith(`/v/${TALK}`)
    expect(join).toHaveBeenCalledWith(TALK)
  })

  it('ignores a Talk that another lobby opened', async () => {
    const { cx } = world()
    const join = vi.fn(async () => {})
    const joined = joinViaLobby(cx, LOBBY, join)
    lobbyMoved(cx, { from_channel_id: fixtureId(77), channel_id: TALK })
    expect(join).not.toHaveBeenCalled()
    lobbyRefused(cx, { channel_id: LOBBY, reason: 'talk_unavailable' })
    await joined
    expect(join).not.toHaveBeenCalled()
  })

  it('tells the member why no Talk opened and joins nothing', async () => {
    const { cx } = world()
    const join = vi.fn(async () => {})
    const joined = joinViaLobby(cx, LOBBY, join)
    expect(lobbyRefused(cx, { channel_id: LOBBY, reason: 'talk_rate_limited' })).toBe(true)
    await joined
    expect(join).not.toHaveBeenCalled()
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual(['You’ve opened a lot of Talks lately. Try again in a few minutes.'])
  })

  it('does not claim a refusal that is not about the waiting lobby', () => {
    const { cx } = world()
    expect(lobbyRefused(cx, { channel_id: LOBBY, reason: 'room_full' })).toBe(false)
  })

  it('gives up when the server never answers', async () => {
    const { cx } = world()
    const join = vi.fn(async () => {})
    const joined = joinViaLobby(cx, LOBBY, join)
    await vi.advanceTimersByTimeAsync(10_001)
    await joined
    expect(join).not.toHaveBeenCalled()
    expect(useToastStore().toasts).toHaveLength(1)
    expect(cx.st.pendingLobby).toBeNull()
  })
})
