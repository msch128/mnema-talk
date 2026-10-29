import { afterEach, describe, expect, it, vi } from 'vitest'
import { useVoiceSettings } from './voiceSettings'

afterEach(() => { vi.restoreAllMocks() })

describe('voice settings with blocked storage', () => {
  it('falls back to the defaults and keeps the settings for the session', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('blocked', 'SecurityError') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('full', 'QuotaExceededError') })
    const settings = useVoiceSettings()
    expect(settings.inputMode.value).toBe('activity')
    expect(settings.noiseMode.value).toBe('ai')
    expect(settings.outputVolume.value).toBe(100)
    settings.outputVolume.value = 40
    expect(() => settings.saveSettings()).not.toThrow()
    expect(settings.outputVolume.value).toBe(40)
  })
})
