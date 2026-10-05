import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useVoiceStore } from './voice'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('per-user volume and local mute', () => {
  it('defaults to 100 % and not muted', () => {
    const voice = useVoiceStore()
    expect(voice.getUserVolume('u1')).toBe(100)
    expect(voice.isUserLocalMuted('u1')).toBe(false)
  })

  it('stores a volume per user, clamped to 0..200', () => {
    const voice = useVoiceStore()
    voice.setUserVolume('u1', 150)
    voice.setUserVolume('u2', 999)
    voice.setUserVolume('u3', -5)
    voice.setUserVolume('u4', 'nope')
    expect(voice.getUserVolume('u1')).toBe(150)
    expect(voice.getUserVolume('u2')).toBe(200)
    expect(voice.getUserVolume('u3')).toBe(0)
    expect(voice.getUserVolume('u4')).toBe(100)
  })

  it('persists across store instances and drops default entries', () => {
    const voice = useVoiceStore()
    voice.setUserVolume('u1', 40)
    voice.toggleLocalMute('u2')
    setActivePinia(createPinia())
    const again = useVoiceStore()
    expect(again.getUserVolume('u1')).toBe(40)
    expect(again.isUserLocalMuted('u2')).toBe(true)

    again.setUserVolume('u1', 100)
    again.toggleLocalMute('u2')
    expect(JSON.parse(localStorage.getItem('mnema_user_volumes'))).toEqual({})
    expect(JSON.parse(localStorage.getItem('mnema_user_muted'))).toEqual({})
  })

  it('still works when storage is unavailable', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    voice.setUserVolume('u1', 70)
    voice.toggleLocalMute('u1')
    expect(voice.getUserVolume('u1')).toBe(70)
    expect(voice.isUserLocalMuted('u1')).toBe(true)
    getItem.mockRestore()
    setItem.mockRestore()
  })

  it('ignores corrupt stored values', () => {
    localStorage.setItem('mnema_user_volumes', '[1,2')
    localStorage.setItem('mnema_user_muted', '"x"')
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    expect(voice.getUserVolume('u1')).toBe(100)
    expect(voice.isUserLocalMuted('u1')).toBe(false)
  })
})

describe('participant video streams', () => {
  it('keeps a newer stream when a stale one is removed', () => {
    const voice = useVoiceStore()
    const oldStream = {}
    const newStream = {}
    voice.setUserVideoStream('u1', oldStream)
    voice.setUserVideoStream('u1', newStream)
    voice.removeUserVideoStream('u1', oldStream)
    expect(voice.userVideoStreams.u1).toBe(newStream)
    voice.removeUserVideoStream('u1', newStream)
    expect(voice.userVideoStreams.u1).toBeUndefined()
  })

  it('drops a participant camera when they leave', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('u1', {})
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'c', user_id: 'u1' })
    expect(voice.userVideoStreams.u1).toBeUndefined()
  })
})
