import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { playSound } from './soundEffects'
import { useVoiceStore } from '../stores/voice'

class MockAudioNode {
  connect = vi.fn()
  disconnect = vi.fn()
}

class MockAudioParam {
  value = 1
  setValueAtTime = vi.fn()
  linearRampToValueAtTime = vi.fn()
  exponentialRampToValueAtTime = vi.fn()
}

class MockOscillator extends MockAudioNode {
  type = 'sine'
  frequency = new MockAudioParam()
  start = vi.fn()
  stop = vi.fn()
}

class MockGain extends MockAudioNode {
  gain = new MockAudioParam()
}

class MockBiquadFilter extends MockAudioNode {
  type = 'lowpass'
  frequency = new MockAudioParam()
}

class MockAudioContext {
  static sinkCalls = []
  currentTime = 10
  sinkId = ''
  setSinkId(id) {
    MockAudioContext.sinkCalls.push(id)
    this.sinkId = id
    return Promise.resolve()
  }
  destination = new MockAudioNode()
  createOscillator() { return new MockOscillator() }
  createGain() { return new MockGain() }
  createBiquadFilter() { return new MockBiquadFilter() }
  resume = vi.fn().mockResolvedValue()
}

describe('soundEffects', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal('AudioContext', MockAudioContext)
  })

  it('plays all supported sound effect names without errors', () => {
    const sounds = ['join', 'leave', 'mute', 'unmute', 'deafen', 'undeafen', 'user_join', 'user_leave', 'ptt_start', 'ptt_stop']
    for (const name of sounds) {
      expect(() => playSound(name)).not.toThrow()
    }
  })

  it('respects granular soundEvents disabled for a specific sound', () => {
    const voice = useVoiceStore()
    voice.soundEffectsEnabled = true
    voice.soundEvents.mute = false
    const spy = vi.spyOn(MockAudioContext.prototype, 'createGain')

    playSound('mute')
    expect(spy).not.toHaveBeenCalled()

    playSound('unmute')
    expect(spy).toHaveBeenCalled()
  })

  it('plays sound when force is true even if disabled', () => {
    const voice = useVoiceStore()
    voice.soundEffectsEnabled = false
    const spy = vi.spyOn(MockAudioContext.prototype, 'createGain')

    playSound('join', null, true)
    expect(spy).toHaveBeenCalled()
  })

  it('respects soundEffectsEnabled false', () => {
    const voice = useVoiceStore()
    voice.soundEffectsEnabled = false
    const ctx = new MockAudioContext()
    const spy = vi.spyOn(ctx, 'createGain')
    vi.stubGlobal('AudioContext', function() { return ctx })

    playSound('join')
    expect(spy).not.toHaveBeenCalled()
  })

  it('respects zero volume', () => {
    const voice = useVoiceStore()
    voice.soundEffectsEnabled = true
    voice.soundEffectsVolume = 0

    const ctx = new MockAudioContext()
    const spy = vi.spyOn(ctx, 'createGain')
    vi.stubGlobal('AudioContext', function() { return ctx })

    playSound('join')
    expect(spy).not.toHaveBeenCalled()
  })

  it('gracefully handles missing window/AudioContext', () => {
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', undefined)
    expect(() => playSound('join')).not.toThrow()
  })

  it('plays on the output device chosen for voices', () => {
    const voice = useVoiceStore()
    voice.soundEffectsEnabled = true
    voice.selectedOutputDeviceId = 'headset'
    playSound('join')
    expect(MockAudioContext.sinkCalls.at(-1)).toBe('headset')
    const calls = MockAudioContext.sinkCalls.length
    playSound('join')
    // Already there: not switched again.
    expect(MockAudioContext.sinkCalls.length).toBe(calls)
  })
})
