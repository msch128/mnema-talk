import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
const mocks = vi.hoisted(() => ({ invoke: vi.fn(), mute: vi.fn(), deafen: vi.fn(), save: vi.fn(), stop: vi.fn(), lease: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => ({ invoke: mocks.invoke }))
const account = '11111111-1111-4111-8111-111111111111'
const channel = '22222222-2222-4222-8222-222222222222'
const auth = reactive({ user: { id: account } as { id: string } | null })
const chat = reactive({ isConnected: true })
const voice = reactive({ isConnected: true, currentChannelId: channel as string | null, rtcStats: { connected: true }, channelUsers: { [channel]: { [account]: { id: account, username: 'Player', display_name: '' } } }, isMuted: false, isDeafened: false, isScreenSharing: false, inputMode: 'ptt', isPttPressed: false, showScreenShareModal: false, mediaState: {} as Record<string, { screen: boolean }>, isSpeaking: () => true, muteStateOf: () => ({ muted: false }), toggleMute: mocks.mute, toggleDeafen: mocks.deafen, saveSettings: mocks.save })
vi.mock('../stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('../stores/chat', () => ({ useChatStore: () => chat }))
vi.mock('../stores/voice', () => ({ useVoiceStore: () => voice }))
vi.mock('../composables/useWebRTC', () => ({ setGamingPttLease: mocks.lease, useWebRTC: () => ({ stopScreenShare: mocks.stop }) }))
import { chordFromKey, chordText, gamingSurface, startDesktopGaming } from './desktopGaming'
let stop = () => {}
function event(action: string, scope: unknown, token = 'test-window') { const issued = performance.now(); window.dispatchEvent(new CustomEvent('mnema-gaming-command', { detail: { token, scope, action, issued, expires: issued + 150 } })) }
async function flush() { await Promise.resolve(); await Promise.resolve() }
beforeEach(() => {
  vi.useFakeTimers(); vi.clearAllMocks(); mocks.invoke.mockResolvedValue(undefined)
  vi.stubGlobal('location', { protocol: 'https:' })
  Reflect.set(window, '__MNEMA_GAMING_BRIDGE__', 'test-window')
  auth.user = { id: account }; chat.isConnected = true; voice.isConnected = true; voice.currentChannelId = channel; voice.rtcStats.connected = true; voice.isPttPressed = false; voice.isScreenSharing = false; voice.inputMode = 'ptt'; voice.showScreenShareModal = false
})
afterEach(() => { stop(); stop = () => {}; vi.unstubAllGlobals(); Reflect.deleteProperty(window, '__MNEMA_GAMING_BRIDGE__'); Reflect.deleteProperty(window, '__MNEMA_GAMING_SURFACE__'); vi.useRealTimers() })
describe('desktop gaming session bridge', () => {
  it('stays inactive in browsers and recognizes only its local surfaces', () => {
    Reflect.deleteProperty(window, '__MNEMA_GAMING_BRIDGE__'); startDesktopGaming()(); expect(mocks.invoke).not.toHaveBeenCalled()
    Reflect.set(window, '__MNEMA_GAMING_BRIDGE__', 'test-window'); vi.stubGlobal('location', { protocol: 'http:' }); startDesktopGaming()(); expect(mocks.invoke).not.toHaveBeenCalled()
    expect(gamingSurface()).toBe(false); Reflect.set(window, '__MNEMA_GAMING_SURFACE__', 'sidepeek'); expect(gamingSurface()).toBe(true)
  })
  it('reports actual own membership and clears it on disconnect', async () => {
    stop = startDesktopGaming(); await flush()
    expect(mocks.invoke).toHaveBeenCalledWith('desktop_gaming_sync', expect.objectContaining({ voice: expect.objectContaining({ connected: true, account, channel, members: [expect.objectContaining({ name: 'Player', speaking: true })] }) }))
    chat.isConnected = false; await vi.advanceTimersByTimeAsync(500)
    expect(mocks.invoke).toHaveBeenLastCalledWith('desktop_gaming_sync', expect.objectContaining({ voice: expect.objectContaining({ connected: false, members: [] }) }))
  })
  it('binds queued controls to the current session and enforces PTT release', async () => {
    stop = startDesktopGaming(); await flush()
    const scope: unknown = mocks.invoke.mock.calls[0]?.[1]?.voice.scope
    event('ptt-press', scope); expect(voice.isPttPressed).toBe(true)
    await vi.advanceTimersByTimeAsync(301); expect(voice.isPttPressed).toBe(false)
    event('mute', scope); event('deafen', scope); expect(mocks.mute).toHaveBeenCalledOnce(); expect(mocks.deafen).toHaveBeenCalledOnce()
    event('stream', scope); expect(voice.showScreenShareModal).toBe(true)
    voice.isScreenSharing = true; event('stream', scope); expect(mocks.stop).toHaveBeenCalledOnce()
    event('ptt-mode', scope); expect(voice.inputMode).toBe('activity'); expect(mocks.save).toHaveBeenCalledOnce()
    event('ptt-press', scope); expect(voice.isPttPressed).toBe(false)
    auth.user = null; event('mute', scope); expect(mocks.mute).toHaveBeenCalledOnce()
    auth.user = { id: account }; event('mute', scope); expect(mocks.mute).toHaveBeenCalledOnce()
    event('ptt-release', scope); expect(voice.isPttPressed).toBe(false)
    window.dispatchEvent(new Event('mnema-gaming-command')); window.dispatchEvent(new CustomEvent('mnema-gaming-command', { detail: null })); event('mute', scope, 'old-window'); expect(mocks.mute).toHaveBeenCalledOnce()
  })
  it('rejects queued commands after RTC readiness or own membership disappears', async () => {
    stop = startDesktopGaming(); await flush()
    const scope: unknown = mocks.invoke.mock.calls[0]?.[1]?.voice.scope
    voice.rtcStats.connected = false; event('mute', scope); event('ptt-press', scope); expect(mocks.mute).not.toHaveBeenCalled(); expect(voice.isPttPressed).toBe(false)
    voice.rtcStats.connected = true; await vi.advanceTimersByTimeAsync(500)
    const next: unknown = mocks.invoke.mock.lastCall?.[1]?.voice.scope
    const own = voice.channelUsers[channel]![account]!
    Reflect.deleteProperty(voice.channelUsers[channel]!, account); event('stream', next); expect(voice.showScreenShareModal).toBe(false)
    voice.channelUsers[channel]![account] = own
  })
  it('rejects expired or future queued PTT instead of renewing a microphone lease', async () => {
    stop = startDesktopGaming(); await flush(); const scope: unknown = mocks.invoke.mock.calls[0]?.[1]?.voice.scope
    for (const delta of [-1000, 1000]) { const issued = performance.now() + delta; window.dispatchEvent(new CustomEvent('mnema-gaming-command', { detail: { token: 'test-window', scope, action: 'ptt-press', issued, expires: issued + 150 } })) }
    expect(voice.isPttPressed).toBe(false); expect(mocks.lease).not.toHaveBeenCalledWith(true)
  })
  it('rejects malformed command deadlines', async () => {
    stop = startDesktopGaming(); await flush(); const scope: unknown = mocks.invoke.mock.calls[0]?.[1]?.voice.scope; const now = performance.now()
    for (const timing of [{ issued: 'invalid', expires: now + 150 }, { issued: now, expires: 'invalid' }, { issued: NaN, expires: now + 150 }, { issued: now, expires: NaN }, { issued: now, expires: now }, { issued: now, expires: now + 200 }]) window.dispatchEvent(new CustomEvent('mnema-gaming-command', { detail: { token: 'test-window', scope, action: 'ptt-press', ...timing } }))
    expect(voice.isPttPressed).toBe(false)
  })
  it('bounds pending sync and handles absent channels and unknown actions', async () => {
    let finish: (() => void) | undefined
    mocks.invoke.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
    stop = startDesktopGaming(); await vi.advanceTimersByTimeAsync(1500); expect(mocks.invoke).toHaveBeenCalledOnce()
    const scope: unknown = mocks.invoke.mock.calls[0]?.[1]?.voice.scope
    event('unknown', scope); event('ptt-mode', scope); event('ptt-mode', scope); expect(voice.inputMode).toBe('ptt')
    finish?.(); await flush(); mocks.invoke.mockResolvedValue(undefined)
    voice.currentChannelId = null; await vi.advanceTimersByTimeAsync(500); expect(mocks.invoke.mock.lastCall?.[1]?.voice.members).toEqual([])
    voice.currentChannelId = 'absent'; await vi.advanceTimersByTimeAsync(500); expect(mocks.invoke.mock.lastCall?.[1]?.voice.connected).toBe(false)
  })
  it('releases on failed sync, page exit and teardown', async () => {
    mocks.invoke.mockRejectedValue(new Error('fixture failure')); stop = startDesktopGaming(); await flush(); expect(voice.isPttPressed).toBe(false)
    voice.isPttPressed = true; window.dispatchEvent(new Event('pagehide')); expect(voice.isPttPressed).toBe(false)
    voice.isPttPressed = true; stop(); expect(voice.isPttPressed).toBe(false)
  })
  it('disables readiness without transport, membership or account', async () => {
    voice.rtcStats.connected = false; stop = startDesktopGaming(); await flush(); expect(mocks.invoke.mock.calls[0]?.[1]?.voice.connected).toBe(false)
    auth.user = null; await vi.advanceTimersByTimeAsync(500); expect(mocks.invoke.mock.lastCall?.[1]?.voice.connected).toBe(false)
  })
})
it('captures supported chords and rejects reserved keys', () => {
  for (const [code, key] of [['KeyM', 77], ['Digit4', 52], ['F24', 135], ['Space', 32]] as const) expect(chordFromKey(new KeyboardEvent('keydown', { code }))).toMatchObject({ key })
  for (const options of [{ code: 'AltLeft' }, { code: 'KeyM', metaKey: true }, { code: 'F4', altKey: true }, { code: 'Space', altKey: true }]) expect(chordFromKey(new KeyboardEvent('keydown', options))).toBeNull()
  expect(chordText({ key: 77, alt: true, control: false, shift: false })).toBe('Alt+M')
  expect(chordText({ key: 32, alt: false, control: true, shift: true })).toBe('Ctrl+Shift+Space')
  expect(chordText({ key: 112, alt: false, control: false, shift: false })).toBe('F1')
})
