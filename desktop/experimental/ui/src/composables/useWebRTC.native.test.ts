import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWebRTC } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { save as saveVoiceSession } from '../lib/voiceSession'
const capture = vi.fn()
const display = vi.fn()
const network = vi.fn()
const peer = vi.fn()
beforeEach(() => {
  setActivePinia(createPinia())
  capture.mockReset(); display.mockReset(); network.mockReset(); peer.mockReset()
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: capture, getDisplayMedia: display } })
  vi.stubGlobal('fetch', network)
  vi.stubGlobal('RTCPeerConnection', peer)
})
afterEach(() => { vi.unstubAllGlobals(); localStorage.clear() })
it.each(['voice', 'camera', 'screen'] as const)('denies unqualified %s before physical capture or signaling', async action => {
  const rtc = useWebRTC()
  const start = action === 'voice' ? rtc.joinVoiceChannel('00000000-0000-4000-8000-000000000001') : action === 'camera' ? rtc.startCamera() : rtc.startScreenShare()
  await expect(start).rejects.toThrow('noch gesperrt')
  expect(capture).not.toHaveBeenCalled(); expect(display).not.toHaveBeenCalled()
  expect(network).not.toHaveBeenCalled(); expect(peer).not.toHaveBeenCalled()
  expect(useVoiceStore().currentChannelId).toBeNull()
})
it('does not resume a saved browser Voice session without a qualified native grant', async () => {
  saveVoiceSession('00000000-0000-4000-8000-000000000001')
  await useWebRTC().resumeVoiceSession()
  expect(capture).not.toHaveBeenCalled(); expect(display).not.toHaveBeenCalled()
  expect(network).not.toHaveBeenCalled(); expect(peer).not.toHaveBeenCalled()
  expect(useVoiceStore().currentChannelId).toBeNull()
})
