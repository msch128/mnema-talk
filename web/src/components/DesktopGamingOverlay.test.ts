import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import DesktopGamingOverlay from './DesktopGamingOverlay.vue'
import type { GamingSnapshot } from '../lib/desktopGaming'
const ipc = vi.hoisted(() => ({ invoke: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => ({ invoke: ipc.invoke }))
let wrapper: ReturnType<typeof mount<typeof DesktopGamingOverlay>> | undefined
let snapshot: GamingSnapshot
const chord = (key: number, alt = false) => ({ key, alt, control: false, shift: false })
beforeEach(() => {
  vi.useFakeTimers(); vi.clearAllMocks()
  snapshot = { active: true, overlay: false, settings: { overlay: chord(77, true), mute: chord(78, true), deafen: chord(68, true), ptt: chord(32), games: ['Wow.exe'] }, voice: { clock: 0, scope: null, connected: true, account: null, channel: null, muted: false, deafened: false, sharing: false, ptt_mode: false, members: Array.from({ length: 14 }, (_, i) => ({ id: String(i), name: `Player ${i}`, speaking: i === 0, muted: i === 1, sharing: i === 2 })) } }
  ipc.invoke.mockImplementation((command: string) => Promise.resolve(command === 'desktop_gaming_snapshot' ? structuredClone(snapshot) : undefined))
  Reflect.set(window, '__MNEMA_GAMING_SURFACE__', 'sidepeek')
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; Reflect.deleteProperty(window, '__MNEMA_GAMING_SURFACE__'); vi.useRealTimers() })
async function open(interactive = false) { Reflect.set(window, '__MNEMA_GAMING_SURFACE__', interactive ? 'gaming-overlay' : 'sidepeek'); wrapper = mount(DesktopGamingOverlay); await flushPromises(); return wrapper }
describe('native gaming surfaces', () => {
  it('shows a bounded passive roster with speaking/mute/share and hides when inactive', async () => {
    const w = await open(); expect(w.findAll('li')).toHaveLength(12); expect(w.text()).toContain('14'); expect(w.text()).toContain('Alt+M')
    expect(w.findAll('.speaking')).toHaveLength(1); expect(w.findAll('.speaker-dot.live')).toHaveLength(1); expect(w.findAll('button')).toHaveLength(0)
    expect(w.findAll('li')[1]?.find('[aria-label]').exists()).toBe(true); expect(w.findAll('li')[2]?.find('[aria-label]').exists()).toBe(true)
    snapshot.active = false; await vi.advanceTimersByTimeAsync(200); await flushPromises(); expect(w.find('section').exists()).toBe(false)
  })
  it('runs existing controls and displays updated mute/deafen/share states', async () => {
    const w = await open(true); expect(w.findAll('li')).toHaveLength(14)
    const buttons = w.findAll('button'); for (const button of buttons.slice(1)) await button.trigger('click')
    for (const action of ['close', 'mute', 'deafen', 'stream', 'ptt-mode']) expect(ipc.invoke).toHaveBeenCalledWith('desktop_gaming_control', { action })
    snapshot.voice.muted = true; snapshot.voice.deafened = true; snapshot.voice.sharing = true; snapshot.voice.ptt_mode = true
    await vi.advanceTimersByTimeAsync(200); expect(w.findAll('[aria-pressed="true"]')).toHaveLength(4)
    ipc.invoke.mockRejectedValueOnce(new Error('stale')); await w.findAll('button')[2]!.trigger('click'); await flushPromises(); expect(w.find('[role="alert"]').exists()).toBe(true)
  })
  it('edits an independent settings draft, captures chords and persists game basenames', async () => {
    const w = await open(true); await w.findAll('button')[0]!.trigger('click')
    const inputs = w.findAll('input'); expect(inputs).toHaveLength(4)
    await inputs[0]!.trigger('keydown', { code: 'KeyK', ctrlKey: true }); expect(inputs[0]!.element.value).toBe('Ctrl+K')
    await inputs[1]!.trigger('keydown', { code: 'F4', altKey: true }); expect(inputs[1]!.element.value).toBe('Alt+N')
    await w.get('textarea').setValue(' Other.exe\n\nSecond.exe '); await w.get('form').trigger('submit'); await flushPromises()
    expect(ipc.invoke).toHaveBeenCalledWith('desktop_gaming_settings', { settings: { ...snapshot.settings, overlay: { ...chord(75), control: true }, games: ['Other.exe', 'Second.exe'] } })
  })
  it('shows save failures without discarding the draft and clears unavailable snapshots', async () => {
    const w = await open(true); await w.findAll('button')[0]!.trigger('click'); ipc.invoke.mockRejectedValueOnce(new Error('duplicate')); await w.get('form').trigger('submit'); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true); expect(w.find('form').exists()).toBe(true)
    await w.findAll('button')[0]!.trigger('click'); expect(w.find('form').exists()).toBe(false)
    ipc.invoke.mockRejectedValueOnce(new Error('unavailable')); await vi.advanceTimersByTimeAsync(200); await flushPromises(); expect(w.find('section').exists()).toBe(false)
  })
  it('handles stale settings and submit events after snapshots become unavailable', async () => {
    const w = await open(true); const button = w.findAll('button')[0]!
    await button.trigger('click'); const form = w.get('form')
    ipc.invoke.mockRejectedValueOnce(new Error('unavailable')); await vi.advanceTimersByTimeAsync(200); await flushPromises()
    await button.trigger('click'); await form.trigger('submit'); expect(w.find('section').exists()).toBe(false)
    expect(ipc.invoke.mock.calls.some(call => call[0] === 'desktop_gaming_settings')).toBe(false)
  })
  it('does not overlap pending polling and ignores responses after unmount', async () => {
    let resolve: ((value: GamingSnapshot) => void) | undefined
    ipc.invoke.mockReturnValue(new Promise< GamingSnapshot >(done => { resolve = done }))
    const w = await open(); expect(Reflect.get(w.vm, 'members')).toEqual([]); await vi.advanceTimersByTimeAsync(600); expect(ipc.invoke).toHaveBeenCalledOnce(); w.unmount(); wrapper = undefined
    resolve?.(snapshot); await flushPromises(); await vi.advanceTimersByTimeAsync(400); expect(ipc.invoke).toHaveBeenCalledOnce()
  })
})
