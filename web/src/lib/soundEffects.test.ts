import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import type { SoundEvent } from './soundEffects'

class MockAudioNode {
  connect = vi.fn()
  disconnect = vi.fn()
}
class MockAudioParam {
  value = 1
  setValueAtTime: ReturnType<typeof vi.fn> | undefined = vi.fn()
  linearRampToValueAtTime: ReturnType<typeof vi.fn> | undefined = vi.fn()
  exponentialRampToValueAtTime: ReturnType<typeof vi.fn> | undefined = vi.fn()
}
class MockOscillator extends MockAudioNode {
  type = 'sine'
  frequency: MockAudioParam | undefined = new MockAudioParam()
  start = vi.fn()
  stop = vi.fn()
}
class MockGain extends MockAudioNode { gain: MockAudioParam | undefined = new MockAudioParam() }
class MockBiquadFilter extends MockAudioNode { type = ''; frequency: MockAudioParam | undefined = new MockAudioParam() }
class MockAudioContext {
  static instances: MockAudioContext[] = []
  state = 'running'
  currentTime = 10
  sinkId = ''
  setSinkId = vi.fn(async (id: string) => { this.sinkId = id })
  destination = new MockAudioNode()
  oscillators: MockOscillator[] = []
  gains: MockGain[] = []
  filters: MockBiquadFilter[] = []
  createOscillator = vi.fn((): MockOscillator | null => {
    const oscillator = new MockOscillator()
    this.oscillators.push(oscillator)
    return oscillator
  })
  createGain = vi.fn((): MockGain | null => {
    const gain = new MockGain()
    this.gains.push(gain)
    return gain
  })
  createBiquadFilter = vi.fn((): MockBiquadFilter | null => {
    const filter = new MockBiquadFilter()
    this.filters.push(filter)
    return filter
  })
  resume = vi.fn(async () => {})
  constructor() { MockAudioContext.instances.push(this) }
}
import { present } from '../media-test.fixture'
let playSoundEffect: typeof import('./soundEffects')['playSoundEffect']
let voice: ReturnType<typeof import('../stores/voice')['useVoiceStore']>
function context() { return present(MockAudioContext.instances.at(-1)) }
beforeEach(async () => {
  vi.resetModules()
  setActivePinia(createPinia())
  MockAudioContext.instances = []
  vi.stubGlobal('AudioContext', MockAudioContext)
  vi.stubGlobal('webkitAudioContext', undefined)
  playSoundEffect = (await import('./soundEffects')).playSoundEffect
  voice = (await import('../stores/voice')).useVoiceStore()
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('sound effect envelopes', () => {
  it.each<[SoundEvent, number[]]>([
    ['join', [392, 523.25, 659.25, 1046.5]], ['leave', [659.25, 523.25, 392]],
    ['mute', [460]], ['unmute', [340]], ['deafen', [360]], ['undeafen', [240]],
    ['user_join', [523.25, 659.25]], ['user_leave', [659.25, 523.25]],
    ['ptt_start', [650]], ['ptt_stop', [850]]
  ])('schedules %s with its characteristic frequencies and bounded attack/release', (name, frequencies) => {
    playSoundEffect(name)
    const ctx = context()
    expect(ctx.oscillators).toHaveLength(frequencies.length)
    ctx.oscillators.forEach((oscillator, index) => {
      expect(oscillator.frequency?.setValueAtTime).toHaveBeenCalledWith(frequencies[index], expect.any(Number))
      expect(oscillator.start).toHaveBeenCalledOnce()
      expect(oscillator.stop).toHaveBeenCalledOnce()
      const start = present(oscillator.start.mock.calls[0]?.[0])
      const stop = present(oscillator.stop.mock.calls[0]?.[0])
      expect(stop).toBeGreaterThan(start)
    })
    expect(ctx.gains[0]?.gain?.setValueAtTime).toHaveBeenCalledWith(0.8, 10)
    expect(ctx.gains[0]?.connect).toHaveBeenCalledWith(ctx.destination)
    for (const gain of ctx.gains.slice(1)) {
      expect(gain.gain?.setValueAtTime).toHaveBeenCalledWith(0.0001, expect.any(Number))
      expect(gain.gain?.linearRampToValueAtTime).toHaveBeenCalledOnce()
      expect(gain.gain?.exponentialRampToValueAtTime).toHaveBeenCalledWith(0.0001, expect.any(Number))
    }
  })
  it('applies the deafen filter and still plays when that node is unavailable', () => {
    playSoundEffect('deafen')
    const ctx = context()
    expect(ctx.filters[0]?.type).toBe('lowpass')
    expect(ctx.filters[0]?.frequency?.setValueAtTime).toHaveBeenCalledWith(1000, 10)
    ctx.createBiquadFilter.mockReturnValueOnce(null)
    playSoundEffect('deafen')
    expect(ctx.oscillators).toHaveLength(2)
  })
  it('ramps mute pitch down, clamps overrides and routes to the selected speakers once', () => {
    voice.selectedOutputDeviceId = 'headset'
    playSoundEffect('mute', 2)
    const ctx = context()
    expect(ctx.setSinkId).toHaveBeenCalledWith('headset')
    expect(ctx.gains[0]?.gain?.setValueAtTime).toHaveBeenCalledWith(1, 10)
    expect(ctx.oscillators[0]?.frequency?.exponentialRampToValueAtTime).toHaveBeenCalledWith(340, 10.09)
    playSoundEffect('mute')
    expect(ctx.setSinkId).toHaveBeenCalledOnce()
  })
})

describe('sound playback lifecycle and browser failures', () => {
  it('respects disabled effects, per-event switches, mute volume and force playback', () => {
    voice.soundEffectsEnabled = false
    playSoundEffect('join')
    voice.soundEffectsEnabled = true
    voice.soundEvents.mute = false
    playSoundEffect('mute')
    voice.soundEffectsVolume = 0
    playSoundEffect('unmute')
    playSoundEffect('join', -1)
    expect(MockAudioContext.instances).toHaveLength(0)
    playSoundEffect('join', 0.5, true)
    expect(context().gains[0]?.gain?.setValueAtTime).toHaveBeenCalledWith(0.5, 10)
  })
  it('uses default volume outside Pinia and resumes suspended contexts, swallowing resume rejection', async () => {
    setActivePinia(undefined)
    playSoundEffect('mute')
    const ctx = context()
    expect(ctx.gains[0]?.gain?.setValueAtTime).toHaveBeenCalledWith(0.8, 10)
    ctx.state = 'suspended'
    ctx.resume.mockRejectedValueOnce(new Error('autoplay denied'))
    playSoundEffect('mute')
    await Promise.resolve()
    expect(ctx.resume).toHaveBeenCalledOnce()
  })
  it('recreates a closed context and uses the legacy constructor when necessary', () => {
    playSoundEffect('join')
    context().state = 'closed'
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', MockAudioContext)
    playSoundEffect('leave')
    expect(MockAudioContext.instances).toHaveLength(2)
  })
  it('handles absent Web Audio, absent window and constructor failures', () => {
    vi.stubGlobal('AudioContext', undefined)
    playSoundEffect('join')
    expect(MockAudioContext.instances).toHaveLength(0)
    vi.stubGlobal('AudioContext', class { constructor() { throw new Error('device blocked') } })
    expect(() => playSoundEffect('join')).not.toThrow()
    vi.stubGlobal('window', undefined)
    expect(() => playSoundEffect('join')).not.toThrow()
  })
  it('handles master gain, tone gain and oscillator failures without scheduling broken nodes', () => {
    playSoundEffect('mute')
    const ctx = context()
    ctx.createGain.mockReturnValueOnce(null)
    playSoundEffect('mute')
    expect(ctx.oscillators).toHaveLength(1)
    ctx.createOscillator.mockReturnValueOnce(null)
    playSoundEffect('mute')
    expect(ctx.oscillators).toHaveLength(1)
    ctx.createGain.mockReturnValueOnce(new MockGain()).mockReturnValueOnce(null)
    playSoundEffect('mute')
    expect(ctx.oscillators[1]?.start).not.toHaveBeenCalled()
    ctx.createOscillator.mockImplementationOnce(() => { throw new Error('node failed') })
    expect(() => playSoundEffect('mute')).not.toThrow()
  })
  it('falls back to scalar AudioParam values when automation methods are unavailable', () => {
    playSoundEffect('mute')
    const ctx = context()
    const gain = new MockGain()
    present(gain.gain).setValueAtTime = undefined
    ctx.createGain.mockReturnValue(gain)
    const oscillator = new MockOscillator()
    present(oscillator.frequency).setValueAtTime = undefined
    ctx.createOscillator.mockReturnValue(oscillator)
    ctx.currentTime = 0
    playSoundEffect('unmute')
    expect(gain.gain?.value).toBe(0.5)
    expect(oscillator.frequency?.value).toBe(340)
    expect(oscillator.start).toHaveBeenCalledWith(0)
  })
  it('tolerates unavailable automation curves and missing AudioParam objects', () => {
    playSoundEffect('mute')
    const ctx = context()
    const oscillator = new MockOscillator()
    present(oscillator.frequency).exponentialRampToValueAtTime = undefined
    const gain = new MockGain()
    present(gain.gain).linearRampToValueAtTime = undefined
    present(gain.gain).exponentialRampToValueAtTime = undefined
    ctx.createGain.mockReturnValue(gain)
    ctx.createOscillator.mockReturnValue(oscillator)
    const filter = new MockBiquadFilter()
    filter.frequency = undefined
    ctx.createBiquadFilter.mockReturnValue(filter)
    playSoundEffect('deafen')
    expect(oscillator.start).toHaveBeenCalledOnce()
    oscillator.frequency = undefined
    gain.gain = undefined
    playSoundEffect('unmute')
    expect(oscillator.start).toHaveBeenCalledTimes(2)
  })
})
