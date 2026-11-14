import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createRtcState, type Cx } from './state'
import { cancelPendingLobby, isLobby, joinViaLobby, lobbyMoved, lobbyRefused } from './lobby'
import { useToastStore } from '../../../stores/toast'
import { setLocale } from '../../../i18n'
import { fixtureId } from '../../../test-fixtures.fixture'

const navigate = vi.fn()
const loadPermissions = vi.fn(async () => {})
vi.mock('../../../stores/permissions', () => ({ usePermissionsStore: () => ({ load: loadPermissions }) }))
vi.mock('../../../shared/lib/router', () => ({ navigate: (...args: unknown[]) => navigate(...args) }))

const LOBBY = fixtureId(10)
const TALK = fixtureId(11)

function world(channels: { id: string; is_lobby?: boolean }[] = [{ id: LOBBY, is_lobby: true }, { id: TALK }]) {
  const st = createRtcState()
  const chat = {
    allChannels: channels,
    isConnected: true,
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

  it('joins nothing once the wait was cancelled, even if the server answers later', async () => {
    const { cx, voice } = world()
    const join = vi.fn(async () => {})
    const joined = joinViaLobby(cx, LOBBY, join)
    cancelPendingLobby(cx)
    lobbyMoved(cx, { from_channel_id: LOBBY, channel_id: TALK })
    await joined
    expect(join).not.toHaveBeenCalled()
    expect(navigate).not.toHaveBeenCalled()
    expect(voice.activeView).toBe('chat')
  })

  it('lets a second lobby replace a first one that is already fetching its Talk', async () => {
    const OTHER = fixtureId(12)
    const { cx, chat } = world([{ id: LOBBY, is_lobby: true }, { id: OTHER, is_lobby: true }, { id: TALK }])
    let fetched!: () => void
    chat.fetchChannels.mockImplementationOnce(() => new Promise<void>(resolve => { fetched = resolve }))
    const joinFirst = vi.fn(async () => {})
    const joinSecond = vi.fn(async () => {})
    const first = joinViaLobby(cx, LOBBY, joinFirst)
    lobbyMoved(cx, { from_channel_id: LOBBY, channel_id: TALK })
    await vi.advanceTimersByTimeAsync(0)
    const second = joinViaLobby(cx, OTHER, joinSecond)
    fetched()
    await first
    expect(joinFirst).not.toHaveBeenCalled()
    lobbyMoved(cx, { from_channel_id: OTHER, channel_id: TALK })
    await second
    expect(joinSecond).toHaveBeenCalledWith(TALK)
  })

  it('joins nothing when it is cancelled while the new Talk is being fetched', async () => {
    const { cx, chat } = world()
    let fetched!: () => void
    chat.fetchChannels.mockImplementation(() => new Promise<void>(resolve => { fetched = resolve }))
    const join = vi.fn(async () => {})
    const joined = joinViaLobby(cx, LOBBY, join)
    lobbyMoved(cx, { from_channel_id: LOBBY, channel_id: TALK })
    await vi.advanceTimersByTimeAsync(0)
    cancelPendingLobby(cx)
    fetched()
    await joined
    expect(join).not.toHaveBeenCalled()
    expect(navigate).not.toHaveBeenCalled()
  })
})
