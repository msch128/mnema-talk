import { afterEach, describe, expect, it, vi } from 'vitest'
import { loadStripCollapsed, saveStripCollapsed } from './talkLayout'
import { loadVoiceChatOpen, saveVoiceChatOpen } from './voiceChatPanel'

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('panel persistence without browser storage', () => {
  it('reads default storage and safely handles a missing global', () => {
    localStorage.clear()
    expect(loadStripCollapsed()).toBe(false)
    expect(loadVoiceChatOpen()).toBe(false)
    expect(saveStripCollapsed(true)).toBe(true)
    expect(saveVoiceChatOpen(true)).toBe(true)
    expect(loadStripCollapsed()).toBe(true)
    expect(loadVoiceChatOpen()).toBe(true)
    vi.stubGlobal('localStorage', undefined)
    expect(loadStripCollapsed()).toBe(false)
    expect(loadVoiceChatOpen()).toBe(false)
    expect(saveStripCollapsed(true)).toBe(false)
    expect(saveVoiceChatOpen(true)).toBe(true)
  })
  it('survives browser storage access throwing before getItem can run', () => {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, get: () => { throw new Error('Storage denied') } })
    try {
      expect(loadStripCollapsed()).toBe(false)
      expect(loadVoiceChatOpen()).toBe(false)
      expect(saveStripCollapsed(true)).toBe(false)
      expect(saveVoiceChatOpen(false)).toBe(true)
    } finally {
      if (descriptor) Object.defineProperty(globalThis, 'localStorage', descriptor)
      else Reflect.deleteProperty(globalThis, 'localStorage')
    }
  })
})
