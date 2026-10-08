import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'

const { invoke } = vi.hoisted(() => ({ invoke: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => ({ invoke }))
const wrappers: ReturnType<typeof mount>[] = []
function app() { const w = mount(App); wrappers.push(w); return w }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const candidate = { origin: 'https://example.com', community_id: 'test-community', api_version: 1, login_available: false }

beforeEach(() => { invoke.mockReset(); vi.useFakeTimers() })
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); vi.useRealTimers() })

describe('desktop probe boundaries', () => {
  it('does not contact servers, inspect games or offer credentials on startup', () => {
    const w = app()
    expect(invoke).not.toHaveBeenCalled()
    expect(w.find('input[type=password]').exists()).toBe(false)
    expect(w.findAll('button')[0]?.attributes('disabled')).toBeDefined()
  })

  it('requires an explicit action and shows the confirmed origin without HTML rendering', async () => {
    invoke.mockResolvedValue({ ...candidate, community_id: '<img src=x onerror=alert(1)>' })
    const w = app()
    await w.get('#server').setValue('https://example.com')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(invoke).toHaveBeenCalledWith('inspect_server', { address: 'https://example.com' })
    expect(w.text()).toContain('https://example.com')
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('input[type=password]').exists()).toBe(false)
  })

  it('never displays a raw server/native exception', async () => {
    invoke.mockRejectedValue('private-token-from-error')
    const w = app()
    await w.get('#server').setValue('example.com')
    await w.get('form').trigger('submit'); await flushPromises()
    expect(w.get('[role=alert]').text()).toContain('Keine kompatible')
    expect(w.text()).not.toContain('private-token-from-error')
    expect(w.findAll('button')[0]?.attributes('disabled')).toBeUndefined()
  })

  it('allows only one discovery request at a time, and clears confirmation on address changes', async () => {
    const old = deferred<typeof candidate>()
    invoke.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ ...candidate, community_id: 'current-community' })
    const w = app()
    await w.get('#server').setValue('example.com'); await w.get('form').trigger('submit')
    await w.get('form').trigger('submit')
    expect(invoke).toHaveBeenCalledTimes(1)
    old.resolve({ ...candidate, community_id: 'old-community' }); await flushPromises()
    expect(w.text()).toContain('old-community')
    await w.get('#server').setValue('https://other.example.com')
    expect(w.find('dl').exists()).toBe(false)
    await w.get('form').trigger('submit'); await flushPromises()
    expect(w.text()).toContain('current-community')
    expect(w.text()).not.toContain('old-community')
  })

  it.each(['', ' '.repeat(20), 'a'.repeat(2049)])('rejects empty or oversized discovery input before IPC', async value => {
    const w = app()
    await w.get('#server').setValue(value)
    await w.get('form').trigger('submit')
    expect(invoke).not.toHaveBeenCalled()
    expect(w.get('[role=alert]').text()).toContain('2048')
  })

  it.each(['resolve', 'reject'])('discards discovery %s after disposal', async outcome => {
    const request = deferred<typeof candidate>()
    invoke.mockReturnValue(request.promise)
    const w = app()
    await w.get('#server').setValue('example.com'); await w.get('form').trigger('submit')
    w.unmount()
    if (outcome === 'resolve') request.resolve(candidate)
    else request.reject('private-native-error')
    await flushPromises()
    expect(w.find('dl').exists()).toBe(false)
  })

  it.each(['', ' , ', 'C:\\Games\\Wow.exe', '../Wow.exe', 'Wow..exe', '_Wow.exe', '-Wow.exe', '.Wow.exe', '.exe', 'game.zip', '游戏.exe', `${'a'.repeat(125)}.exe`, Array(33).fill('Wow.exe').join(',')])('requires a bounded basename allowlist: %s', async value => {
    const w = app()
    await w.get('#games').setValue(value)
    expect(w.get('#games').attributes('aria-invalid')).toBe('true')
    expect(w.get('#games-validation').text()).toContain('keine Pfade')
    expect(w.findAll('button')[1]!.attributes('disabled')).toBeDefined()
    await w.findAll('button')[1]!.trigger('click')
    expect(invoke).not.toHaveBeenCalled()
  })

  it('accepts explicit case-insensitive executable basenames without sending paths', async () => {
    invoke.mockResolvedValue({ platform: 'windows', game: null, covers_monitor: false, exclusive_fullscreen_verified: false })
    const w = app()
    await w.get('#games').setValue(' , Test-Game_1.EXE ,  Other Game.exe ,')
    await w.findAll('button')[1]!.trigger('click'); await flushPromises()
    expect(invoke).toHaveBeenCalledWith('inspect_game', { names: ['Test-Game_1.EXE', 'Other Game.exe'] })
    expect(w.find('#games-validation').exists()).toBe(false)
  })

  it('polls only after explicit consent, remembers local game evidence, and clears it on stop', async () => {
    invoke.mockResolvedValueOnce({ platform: 'windows', game: 'Wow.exe', covers_monitor: true, exclusive_fullscreen_verified: false })
      .mockResolvedValue({ platform: 'windows', game: null, covers_monitor: false, exclusive_fullscreen_verified: false })
    const w = app()
    await w.findAll('button')[1]!.trigger('click'); await flushPromises()
    expect(invoke).toHaveBeenCalledWith('inspect_game', { names: ['Wow.exe', 'GenshinImpact.exe'] })
    expect(w.text()).toContain('Zuletzt lokal erkannt: Wow.exe')
    await vi.advanceTimersByTimeAsync(1000); await flushPromises()
    expect(w.text()).toContain('Kein freigegebenes Spiel')
    expect(w.text()).toContain('Zuletzt lokal erkannt: Wow.exe')
    await w.findAll('button')[1]!.trigger('click')
    const calls = invoke.mock.calls.length
    await vi.advanceTimersByTimeAsync(3000)
    expect(invoke.mock.calls).toHaveLength(calls)
    expect(w.text()).not.toContain('Zuletzt lokal erkannt')
  })

  it('does not resurrect stopped polling from an in-flight native request', async () => {
    const pending = deferred<unknown>()
    invoke.mockReturnValue(pending.promise)
    const w = app()
    await w.findAll('button')[1]!.trigger('click')
    await vi.advanceTimersByTimeAsync(2000)
    expect(invoke).toHaveBeenCalledTimes(1)
    await w.findAll('button')[1]!.trigger('click')
    pending.resolve({ platform: 'windows', game: 'Wow.exe', covers_monitor: true, exclusive_fullscreen_verified: false })
    await flushPromises()
    expect(w.text()).not.toContain('Zuletzt lokal erkannt')
  })

  it('handles unsupported hosts and cleans up polls on disposal', async () => {
    invoke.mockResolvedValue({ platform: 'unsupported_probe_host', game: null, covers_monitor: false, exclusive_fullscreen_verified: false })
    const w = app()
    await w.findAll('button')[1]!.trigger('click'); await flushPromises()
    expect(w.text()).toContain('benötigt Windows')
    expect(w.findAll('button')[1]!.text()).toBe('Spielprüfung starten')
    await vi.advanceTimersByTimeAsync(3000)
    expect(invoke).toHaveBeenCalledTimes(1)
    w.unmount()
    const calls = invoke.mock.calls.length
    await vi.advanceTimersByTimeAsync(3000)
    expect(invoke.mock.calls).toHaveLength(calls)
  })

  it('clears historical evidence on native errors and recovers after a fresh check', async () => {
    const game = { platform: 'windows', game: 'Wow.exe', covers_monitor: false, exclusive_fullscreen_verified: false }
    invoke.mockResolvedValueOnce(game).mockRejectedValueOnce('private-local-path').mockResolvedValue(game)
    const w = app()
    await w.findAll('button')[1]!.trigger('click'); await flushPromises()
    expect(w.text()).toContain('Geometrie: nein')
    await vi.advanceTimersByTimeAsync(1000); await flushPromises()
    expect(w.text()).not.toContain('Zuletzt lokal erkannt')
    expect(w.text()).not.toContain('private-local-path')
    expect(w.get('[role=alert]').text()).toContain('lokale Spielprüfung')
    await vi.advanceTimersByTimeAsync(1000); await flushPromises()
    expect(w.find('[role=alert]').exists()).toBe(false)
    expect(w.text()).toContain('Zuletzt lokal erkannt: Wow.exe')
  })

  it('keeps a restarted poll isolated from an old failing request', async () => {
    const request = deferred<unknown>()
    invoke.mockReturnValueOnce(request.promise).mockResolvedValue({ platform: 'windows', game: 'Wow.exe', covers_monitor: true, exclusive_fullscreen_verified: false })
    const w = app()
    await w.findAll('button')[1]!.trigger('click')
    await w.findAll('button')[1]!.trigger('click')
    await w.findAll('button')[1]!.trigger('click')
    expect(invoke).toHaveBeenCalledTimes(1)
    request.reject('private-local-path'); await flushPromises()
    expect(w.find('[role=alert]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(1000); await flushPromises()
    expect(w.text()).toContain('Zuletzt lokal erkannt: Wow.exe')
  })

  it('ships local license notices as text and clearly describes the development boundary', () => {
    const w = app()
    expect(w.get('details').text()).toContain('Open-Source')
    expect(w.get('pre').text()).toContain('Vue')
    expect(w.get('footer').text()).toContain('kein veröffentlichter 0.7-Client')
    expect(w.text()).toContain('startet keine Aufnahme')
  })
})
